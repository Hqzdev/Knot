package objectstore

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	serviceName      = "s3"
	requestType      = "aws4_request"
	algorithm        = "AWS4-HMAC-SHA256"
	unsignedPayload  = "UNSIGNED-PAYLOAD"
	emptyPayloadHash = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	maxPresignTTL    = 7 * 24 * time.Hour
	maxMediaSize     = 5 << 20
)

type S3Config struct {
	Endpoint        string
	PublicEndpoint  string
	Region          string
	Bucket          string
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
	PathStyle       bool
	HTTPClient      *http.Client
}

type S3Store struct {
	endpoint        *url.URL
	publicEndpoint  *url.URL
	region          string
	bucket          string
	accessKeyID     string
	secretAccessKey string
	sessionToken    string
	pathStyle       bool
	client          *http.Client
	now             func() time.Time
}

type queryValue struct {
	key   string
	value string
}

func NewS3Store(config S3Config) (*S3Store, error) {
	return newS3Store(config, time.Now)
}

func newS3Store(config S3Config, now func() time.Time) (*S3Store, error) {
	endpoint, err := parseEndpoint(config.Endpoint)
	if err != nil {
		return nil, errors.New("invalid S3 endpoint")
	}
	publicEndpoint := endpoint
	if config.PublicEndpoint != "" {
		publicEndpoint, err = parseEndpoint(config.PublicEndpoint)
		if err != nil {
			return nil, errors.New("invalid public S3 endpoint")
		}
	}
	if !validBucket(config.Bucket) || !validRegion(config.Region) || !validAccessKey(config.AccessKeyID) || !validSecret(config.SecretAccessKey) || !validSessionToken(config.SessionToken) || now == nil {
		return nil, errors.New("invalid S3 configuration")
	}
	client := config.HTTPClient
	if client == nil {
		client = &http.Client{
			Timeout: 15 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}
	return &S3Store{
		endpoint:        endpoint,
		publicEndpoint:  publicEndpoint,
		region:          config.Region,
		bucket:          config.Bucket,
		accessKeyID:     config.AccessKeyID,
		secretAccessKey: config.SecretAccessKey,
		sessionToken:    config.SessionToken,
		pathStyle:       config.PathStyle,
		client:          client,
		now:             now,
	}, nil
}

func (store *S3Store) PresignUpload(ctx context.Context, objectKey string, size int64, fileSHA256 string, ttl time.Duration) (SignedRequest, error) {
	if err := ctx.Err(); err != nil {
		return SignedRequest{}, err
	}
	if size <= 0 || !validSHA256(fileSHA256) {
		return SignedRequest{}, errors.New("invalid upload parameters")
	}
	headers := map[string]string{
		"content-length":    strconv.FormatInt(size, 10),
		"x-amz-meta-sha256": fileSHA256,
	}
	return store.presign(http.MethodPut, objectKey, headers, ttl)
}

func (store *S3Store) PresignDownload(ctx context.Context, objectKey string, ttl time.Duration) (SignedRequest, error) {
	if err := ctx.Err(); err != nil {
		return SignedRequest{}, err
	}
	return store.presign(http.MethodGet, objectKey, nil, ttl)
}

func (store *S3Store) Head(ctx context.Context, objectKey string) (ObjectInfo, error) {
	request, err := store.signedRequest(ctx, http.MethodHead, objectKey, false)
	if err != nil {
		return ObjectInfo{}, err
	}
	response, err := store.client.Do(request)
	if err != nil {
		return ObjectInfo{}, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return ObjectInfo{}, ErrObjectNotFound
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return ObjectInfo{}, fmt.Errorf("S3 HEAD returned status %d", response.StatusCode)
	}
	return ObjectInfo{Size: response.ContentLength, SHA256: strings.ToLower(response.Header.Get("X-Amz-Meta-Sha256"))}, nil
}

func (store *S3Store) Put(ctx context.Context, objectKey string, mediaType string, data []byte) error {
	if len(data) == 0 || len(data) > maxMediaSize || mediaType == "" {
		return errors.New("invalid media object")
	}
	request, err := store.signedPayloadRequest(ctx, http.MethodPut, objectKey, mediaType, data)
	if err != nil {
		return err
	}
	response, err := store.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("S3 PUT returned status %d", response.StatusCode)
	}
	return nil
}

func (store *S3Store) Get(ctx context.Context, objectKey string) (MediaObject, error) {
	request, err := store.signedRequest(ctx, http.MethodGet, objectKey, false)
	if err != nil {
		return MediaObject{}, err
	}
	response, err := store.client.Do(request)
	if err != nil {
		return MediaObject{}, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return MediaObject{}, ErrObjectNotFound
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return MediaObject{}, fmt.Errorf("S3 GET returned status %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxMediaSize+1))
	if err != nil {
		return MediaObject{}, err
	}
	if len(data) == 0 || len(data) > maxMediaSize {
		return MediaObject{}, errors.New("invalid media object")
	}
	return MediaObject{Data: data, MediaType: response.Header.Get("Content-Type")}, nil
}

func (store *S3Store) Delete(ctx context.Context, objectKey string) error {
	request, err := store.signedRequest(ctx, http.MethodDelete, objectKey, false)
	if err != nil {
		return err
	}
	response, err := store.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound || response.StatusCode >= 200 && response.StatusCode < 300 {
		return nil
	}
	return fmt.Errorf("S3 DELETE returned status %d", response.StatusCode)
}

func (store *S3Store) Ping(ctx context.Context) error {
	request, err := store.signedRequest(ctx, http.MethodHead, "", true)
	if err != nil {
		return err
	}
	response, err := store.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("S3 bucket HEAD returned status %d", response.StatusCode)
	}
	return nil
}

func (store *S3Store) presign(method string, objectKey string, headers map[string]string, ttl time.Duration) (SignedRequest, error) {
	if !validObjectKey(objectKey) || ttl < time.Second || ttl > maxPresignTTL {
		return SignedRequest{}, errors.New("invalid presign parameters")
	}
	now := store.now().UTC()
	expiresAt := now.Add(time.Duration(ttl/time.Second) * time.Second)
	requestURL, canonicalURI, host := store.resource(store.publicEndpoint, objectKey, false)
	canonicalHeaders := map[string]string{"host": host}
	responseHeaders := make(map[string]string, len(headers))
	for name, value := range headers {
		canonicalHeaders[strings.ToLower(name)] = normalizedHeaderValue(value)
		responseHeaders[canonicalHeaderName(name)] = value
	}
	signedHeaders, canonicalHeaderBlock := canonicalizeHeaders(canonicalHeaders)
	date := now.Format("20060102")
	timestamp := now.Format("20060102T150405Z")
	scope := date + "/" + store.region + "/" + serviceName + "/" + requestType
	query := []queryValue{
		{key: "X-Amz-Algorithm", value: algorithm},
		{key: "X-Amz-Credential", value: store.accessKeyID + "/" + scope},
		{key: "X-Amz-Date", value: timestamp},
		{key: "X-Amz-Expires", value: strconv.FormatInt(int64(ttl/time.Second), 10)},
		{key: "X-Amz-SignedHeaders", value: signedHeaders},
	}
	if store.sessionToken != "" {
		query = append(query, queryValue{key: "X-Amz-Security-Token", value: store.sessionToken})
	}
	canonicalQuery := canonicalizeQuery(query)
	canonicalRequest := strings.Join([]string{method, canonicalURI, canonicalQuery, canonicalHeaderBlock, signedHeaders, unsignedPayload}, "\n")
	stringToSign := strings.Join([]string{algorithm, timestamp, scope, sha256Hex(canonicalRequest)}, "\n")
	signature := hex.EncodeToString(hmacSHA256(store.signingKey(date), stringToSign))
	query = append(query, queryValue{key: "X-Amz-Signature", value: signature})
	return SignedRequest{URL: requestURL + "?" + canonicalizeQuery(query), Headers: responseHeaders, ExpiresAt: expiresAt}, nil
}

func (store *S3Store) signedRequest(ctx context.Context, method string, objectKey string, bucketOnly bool) (*http.Request, error) {
	if !bucketOnly && !validObjectKey(objectKey) {
		return nil, errors.New("invalid object key")
	}
	requestURL, canonicalURI, host := store.resource(store.endpoint, objectKey, bucketOnly)
	request, err := http.NewRequestWithContext(ctx, method, requestURL, nil)
	if err != nil {
		return nil, err
	}
	now := store.now().UTC()
	timestamp := now.Format("20060102T150405Z")
	date := now.Format("20060102")
	request.Host = host
	request.Header.Set("X-Amz-Date", timestamp)
	request.Header.Set("X-Amz-Content-Sha256", emptyPayloadHash)
	canonicalHeaders := map[string]string{
		"host":                 host,
		"x-amz-content-sha256": emptyPayloadHash,
		"x-amz-date":           timestamp,
	}
	if store.sessionToken != "" {
		request.Header.Set("X-Amz-Security-Token", store.sessionToken)
		canonicalHeaders["x-amz-security-token"] = store.sessionToken
	}
	signedHeaders, canonicalHeaderBlock := canonicalizeHeaders(canonicalHeaders)
	canonicalRequest := strings.Join([]string{method, canonicalURI, "", canonicalHeaderBlock, signedHeaders, emptyPayloadHash}, "\n")
	scope := date + "/" + store.region + "/" + serviceName + "/" + requestType
	stringToSign := strings.Join([]string{algorithm, timestamp, scope, sha256Hex(canonicalRequest)}, "\n")
	signature := hex.EncodeToString(hmacSHA256(store.signingKey(date), stringToSign))
	request.Header.Set("Authorization", algorithm+" Credential="+store.accessKeyID+"/"+scope+", SignedHeaders="+signedHeaders+", Signature="+signature)
	return request, nil
}

func (store *S3Store) signedPayloadRequest(ctx context.Context, method string, objectKey string, mediaType string, payload []byte) (*http.Request, error) {
	if !validObjectKey(objectKey) {
		return nil, errors.New("invalid object key")
	}
	requestURL, canonicalURI, host := store.resource(store.endpoint, objectKey, false)
	request, err := http.NewRequestWithContext(ctx, method, requestURL, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	now := store.now().UTC()
	timestamp := now.Format("20060102T150405Z")
	date := now.Format("20060102")
	payloadDigest := sha256.Sum256(payload)
	payloadHash := hex.EncodeToString(payloadDigest[:])
	request.Host = host
	request.ContentLength = int64(len(payload))
	request.Header.Set("Content-Type", mediaType)
	request.Header.Set("X-Amz-Date", timestamp)
	request.Header.Set("X-Amz-Content-Sha256", payloadHash)
	canonicalHeaders := map[string]string{
		"content-type":         mediaType,
		"host":                 host,
		"x-amz-content-sha256": payloadHash,
		"x-amz-date":           timestamp,
	}
	if store.sessionToken != "" {
		request.Header.Set("X-Amz-Security-Token", store.sessionToken)
		canonicalHeaders["x-amz-security-token"] = store.sessionToken
	}
	signedHeaders, canonicalHeaderBlock := canonicalizeHeaders(canonicalHeaders)
	canonicalRequest := strings.Join([]string{method, canonicalURI, "", canonicalHeaderBlock, signedHeaders, payloadHash}, "\n")
	scope := date + "/" + store.region + "/" + serviceName + "/" + requestType
	stringToSign := strings.Join([]string{algorithm, timestamp, scope, sha256Hex(canonicalRequest)}, "\n")
	signature := hex.EncodeToString(hmacSHA256(store.signingKey(date), stringToSign))
	request.Header.Set("Authorization", algorithm+" Credential="+store.accessKeyID+"/"+scope+", SignedHeaders="+signedHeaders+", Signature="+signature)
	return request, nil
}

func (store *S3Store) signingKey(date string) []byte {
	dateKey := hmacSHA256([]byte("AWS4"+store.secretAccessKey), date)
	regionKey := hmacSHA256(dateKey, store.region)
	serviceKey := hmacSHA256(regionKey, serviceName)
	return hmacSHA256(serviceKey, requestType)
}

func (store *S3Store) resource(endpoint *url.URL, objectKey string, bucketOnly bool) (string, string, string) {
	host := endpoint.Host
	path := "/"
	if store.pathStyle {
		path += awsEncode(store.bucket, true)
		if !bucketOnly {
			path += "/" + awsEncode(objectKey, false)
		}
	} else {
		host = virtualHost(store.bucket, endpoint)
		if !bucketOnly {
			path += awsEncode(objectKey, false)
		}
	}
	return endpoint.Scheme + "://" + host + path, path, host
}

func parseEndpoint(value string) (*url.URL, error) {
	endpoint, err := url.Parse(value)
	if err != nil || endpoint.Host == "" || endpoint.Scheme != "http" && endpoint.Scheme != "https" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" || endpoint.Path != "" && endpoint.Path != "/" {
		return nil, errors.New("invalid endpoint")
	}
	return endpoint, nil
}

func canonicalizeHeaders(headers map[string]string) (string, string) {
	names := make([]string, 0, len(headers))
	for name := range headers {
		names = append(names, strings.ToLower(name))
	}
	sort.Strings(names)
	var block strings.Builder
	for _, name := range names {
		block.WriteString(name)
		block.WriteByte(':')
		block.WriteString(normalizedHeaderValue(headers[name]))
		block.WriteByte('\n')
	}
	return strings.Join(names, ";"), block.String()
}

func canonicalizeQuery(values []queryValue) string {
	encoded := make([]queryValue, 0, len(values))
	for _, value := range values {
		encoded = append(encoded, queryValue{key: awsEncode(value.key, true), value: awsEncode(value.value, true)})
	}
	sort.Slice(encoded, func(left int, right int) bool {
		if encoded[left].key == encoded[right].key {
			return encoded[left].value < encoded[right].value
		}
		return encoded[left].key < encoded[right].key
	})
	parts := make([]string, 0, len(encoded))
	for _, value := range encoded {
		parts = append(parts, value.key+"="+value.value)
	}
	return strings.Join(parts, "&")
}

func awsEncode(value string, encodeSlash bool) string {
	const hexadecimal = "0123456789ABCDEF"
	var encoded strings.Builder
	for index := 0; index < len(value); index++ {
		character := value[index]
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || strings.ContainsRune("-_.~", rune(character)) || character == '/' && !encodeSlash {
			encoded.WriteByte(character)
			continue
		}
		encoded.WriteByte('%')
		encoded.WriteByte(hexadecimal[character>>4])
		encoded.WriteByte(hexadecimal[character&15])
	}
	return encoded.String()
}

func normalizedHeaderValue(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func canonicalHeaderName(name string) string {
	parts := strings.Split(strings.ToLower(name), "-")
	for index, part := range parts {
		if part != "" {
			parts[index] = strings.ToUpper(part[:1]) + part[1:]
		}
	}
	return strings.Join(parts, "-")
}

func hmacSHA256(key []byte, value string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(value))
	return mac.Sum(nil)
}

func sha256Hex(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validObjectKey(value string) bool {
	if value == "" || len(value) > 1024 || !utf8.ValidString(value) || strings.HasPrefix(value, "/") || strings.Contains(value, "\\") {
		return false
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return false
		}
	}
	return true
}

func validBucket(value string) bool {
	if len(value) < 3 || len(value) > 63 || !asciiAlphanumeric(value[0]) || !asciiAlphanumeric(value[len(value)-1]) || net.ParseIP(value) != nil {
		return false
	}
	for _, character := range value {
		letter := character >= 'a' && character <= 'z'
		digit := character >= '0' && character <= '9'
		if !letter && !digit && character != '-' && character != '.' {
			return false
		}
	}
	return !strings.Contains(value, "..")
}

func asciiAlphanumeric(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= '0' && value <= '9'
}

func validRegion(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for _, character := range value {
		letter := character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z'
		digit := character >= '0' && character <= '9'
		if !letter && !digit && character != '-' {
			return false
		}
	}
	return true
}

func validAccessKey(value string) bool {
	if value == "" || len(value) > 256 {
		return false
	}
	for _, character := range value {
		if character <= 0x20 || character == 0x7f || character == '/' {
			return false
		}
	}
	return true
}

func validSecret(value string) bool {
	return value != "" && len(value) <= 1024 && !strings.ContainsAny(value, "\r\n")
}

func validSessionToken(value string) bool {
	if len(value) > 4096 {
		return false
	}
	for _, character := range value {
		if character <= 0x20 || character == 0x7f {
			return false
		}
	}
	return true
}

func virtualHost(bucket string, endpoint *url.URL) string {
	host := bucket + "." + endpoint.Hostname()
	if endpoint.Port() != "" {
		return net.JoinHostPort(host, endpoint.Port())
	}
	return host
}
