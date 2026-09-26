package cfturn

import (
	"NanoKVM-Server/config"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/pion/webrtc/v4"
	log "github.com/sirupsen/logrus"
)

const (
	defaultBaseURL = "https://rtc.live.cloudflare.com/v1/turn/keys"

	credentialTTL   = 24 * time.Hour
	minRemainingTTL = 12 * time.Hour
	retryInterval   = time.Minute
	requestTimeout  = 5 * time.Second
	maxResponseSize = 64 * 1024
)

type provider struct {
	mutex   sync.Mutex
	client  *http.Client
	baseURL string
	now     func() time.Time

	servers     []webrtc.ICEServer
	expiresAt   time.Time
	nextAttempt time.Time
}

type generateRequest struct {
	TTL int64 `json:"ttl"`
}

type generateResponse struct {
	ICEServers []struct {
		URLs       []string `json:"urls"`
		Username   string   `json:"username"`
		Credential string   `json:"credential"`
	} `json:"iceServers"`
}

var defaultProvider = newProvider(&http.Client{Timeout: requestTimeout}, defaultBaseURL, time.Now)

func newProvider(client *http.Client, baseURL string, now func() time.Time) *provider {
	return &provider{
		client:  client,
		baseURL: baseURL,
		now:     now,
	}
}

func ICEServers() []webrtc.ICEServer {
	conf := config.GetInstance()
	return defaultProvider.get(conf.Turn.CloudflareKeyID, conf.Turn.CloudflareAPIToken)
}

func (p *provider) get(keyID string, apiToken string) []webrtc.ICEServer {
	if keyID == "" || apiToken == "" {
		return nil
	}

	p.mutex.Lock()
	defer p.mutex.Unlock()

	now := p.now()
	if p.servers != nil && now.Add(minRemainingTTL).Before(p.expiresAt) {
		return cloneServers(p.servers)
	}

	if now.Before(p.nextAttempt) {
		return p.validServers(now)
	}

	servers, err := p.fetch(keyID, apiToken)
	if err != nil {
		p.nextAttempt = now.Add(retryInterval)
		log.Errorf("failed to get cloudflare turn credentials: %s", err)
		return p.validServers(now)
	}

	p.servers = servers
	p.expiresAt = now.Add(credentialTTL)
	p.nextAttempt = time.Time{}
	log.Debugf("cloudflare turn credentials refreshed, expires at %s", p.expiresAt.Format(time.RFC3339))

	return cloneServers(p.servers)
}

func (p *provider) validServers(now time.Time) []webrtc.ICEServer {
	if p.servers != nil && now.Before(p.expiresAt) {
		return cloneServers(p.servers)
	}
	return nil
}

func (p *provider) fetch(keyID string, apiToken string) ([]webrtc.ICEServer, error) {
	body, err := json.Marshal(generateRequest{TTL: int64(credentialTTL / time.Second)})
	if err != nil {
		return nil, err
	}

	rawURL := fmt.Sprintf("%s/%s/credentials/generate-ice-servers", p.baseURL, url.PathEscape(keyID))
	req, err := http.NewRequest(http.MethodPost, rawURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("cloudflare returned status %d", resp.StatusCode)
	}

	var data generateResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseSize)).Decode(&data); err != nil {
		return nil, fmt.Errorf("invalid response: %w", err)
	}

	var servers []webrtc.ICEServer
	for _, server := range data.ICEServers {
		if server.Username == "" || server.Credential == "" {
			continue
		}

		urls := filterURLs(server.URLs)
		if len(urls) == 0 {
			continue
		}

		servers = append(servers, webrtc.ICEServer{
			URLs:       urls,
			Username:   server.Username,
			Credential: server.Credential,
		})
	}

	if len(servers) == 0 {
		return nil, errors.New("response contains no turn servers")
	}

	return servers, nil
}

func filterURLs(urls []string) []string {
	filtered := make([]string, 0, len(urls))
	for _, u := range urls {
		if strings.HasSuffix(u, ":53") || strings.Contains(u, ":53?") {
			continue
		}
		filtered = append(filtered, u)
	}
	return filtered
}

func cloneServers(servers []webrtc.ICEServer) []webrtc.ICEServer {
	cloned := make([]webrtc.ICEServer, len(servers))
	for i, server := range servers {
		cloned[i] = server
		cloned[i].URLs = append([]string(nil), server.URLs...)
	}
	return cloned
}
