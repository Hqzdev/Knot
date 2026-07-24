package metadata

import (
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/yaroslavfairfieldd/knot/services/preview/internal/security"
	"golang.org/x/net/html"
)

const maximumResponseBytes = 1 << 20

type Preview struct {
	URL         string `json:"url"`
	Title       string `json:"title"`
	Description string `json:"description"`
	ImageURL    string `json:"image_url"`
	SiteName    string `json:"site_name"`
}

type Fetcher struct {
	guard  *security.Guard
	client *http.Client
}

func NewFetcher(guard *security.Guard) *Fetcher {
	transport := &http.Transport{
		DialContext:           guard.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          20,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 5 * time.Second,
	}
	fetcher := &Fetcher{guard: guard}
	fetcher.client = &http.Client{
		Transport: transport,
		Timeout:   8 * time.Second,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return errors.New("redirect limit exceeded")
			}
			_, err := guard.Validate(request.Context(), request.URL.String())
			return err
		},
	}
	return fetcher
}

func (fetcher *Fetcher) Fetch(ctx context.Context, value string) (Preview, error) {
	target, err := fetcher.guard.Validate(ctx, value)
	if err != nil {
		return Preview{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return Preview{}, err
	}
	request.Header.Set("Accept", "text/html,application/xhtml+xml")
	request.Header.Set("User-Agent", "Knot-Link-Preview/1.0")
	response, err := fetcher.client.Do(request)
	if err != nil {
		return Preview{}, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Preview{}, errors.New("link target returned an unsuccessful status")
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || mediaType != "text/html" && mediaType != "application/xhtml+xml" {
		return Preview{}, errors.New("link target is not HTML")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maximumResponseBytes+1))
	if err != nil || len(body) > maximumResponseBytes {
		return Preview{}, errors.New("link target response is too large")
	}
	preview := parse(body, response.Request.URL)
	preview.URL = response.Request.URL.String()
	if preview.ImageURL != "" {
		if _, validationError := fetcher.guard.Validate(ctx, preview.ImageURL); validationError != nil {
			preview.ImageURL = ""
		}
	}
	return preview, nil
}

func parse(body []byte, baseURL *url.URL) Preview {
	preview := Preview{}
	tokenizer := html.NewTokenizer(strings.NewReader(string(body)))
	inTitle := false
	for {
		tokenType := tokenizer.Next()
		switch tokenType {
		case html.ErrorToken:
			return preview
		case html.StartTagToken, html.SelfClosingTagToken:
			token := tokenizer.Token()
			name := strings.ToLower(token.Data)
			if name == "title" {
				inTitle = true
			}
			if name == "meta" {
				readMeta(&preview, token)
			}
		case html.EndTagToken:
			if strings.EqualFold(tokenizer.Token().Data, "title") {
				inTitle = false
			}
		case html.TextToken:
			if inTitle && preview.Title == "" {
				preview.Title = clean(string(tokenizer.Text()), 200)
			}
		}
		if preview.ImageURL != "" {
			if parsed, err := baseURL.Parse(preview.ImageURL); err == nil {
				preview.ImageURL = parsed.String()
			}
		}
	}
}

func readMeta(preview *Preview, token html.Token) {
	attributes := make(map[string]string, len(token.Attr))
	for _, attribute := range token.Attr {
		attributes[strings.ToLower(attribute.Key)] = strings.TrimSpace(attribute.Val)
	}
	key := strings.ToLower(attributes["property"])
	if key == "" {
		key = strings.ToLower(attributes["name"])
	}
	content := attributes["content"]
	switch key {
	case "og:title", "twitter:title":
		if preview.Title == "" {
			preview.Title = clean(content, 200)
		}
	case "description", "og:description", "twitter:description":
		if preview.Description == "" {
			preview.Description = clean(content, 500)
		}
	case "og:image", "twitter:image":
		if preview.ImageURL == "" {
			preview.ImageURL = strings.TrimSpace(content)
		}
	case "og:site_name":
		preview.SiteName = clean(content, 100)
	}
}

func clean(value string, maximum int) string {
	value = strings.Join(strings.Fields(value), " ")
	characters := []rune(value)
	if len(characters) > maximum {
		characters = characters[:maximum]
	}
	return string(characters)
}
