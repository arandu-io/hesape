// Package s3 is object storage over the S3 protocol.
//
// It is the driver a Disk is given when the files do not live on the machine
// serving them. The package is named for
// the protocol rather than for a vendor, so one implementation covers Cloudflare
// R2, Amazon S3, DigitalOcean Spaces, Backblaze B2 and MinIO.
//
// # Why it is its own module
//
// A project storing on local disk should not carry the S3 protocol
// implementation, and the root module of hesape declares one dependency in
// total. In Go there is no optional dependency, so the only way for
// `go get github.com/arandu-io/hesape` not to bring this along is for this to be
// a module of its own.
//
// # R2 is the default
//
// Use R2 unless something forces otherwise:
//
//	adapter, err := s3.R2(s3.R2Config{
//	    AccountID: cfg.R2AccountID,
//	    Bucket:    "uploads",
//	    AccessKey: cfg.R2AccessKey,
//	    SecretKey: cfg.R2SecretKey,
//	})
//	disks.Add("s3", adapter)
//
// It is the default for a reason that shows up on an invoice: R2 charges no
// egress. For a SaaS that serves files back to the people who uploaded them,
// egress is most of the bill on every other provider, and it is the line that
// grows with success.
//
// # What it is not
//
// There is no SDK here. The S3 protocol is HTTP with a signature, and SigV4 is
// two hundred lines -- against an AWS SDK that brings a hundred modules, its own
// credential chain, its own retry policy and its own context rules. The trade is
// deliberate: this package speaks the operations the [filesystem.Adapter]
// contract needs, and nothing else.
//
// # It never sees a tenant
//
// Every method takes a stored path that [filesystem.Key] already built. That is
// the whole reason the interface is split: this file cannot forget the tenant
// prefix, because it is never told which tenant it is serving.
package s3

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/arandu-io/hesape/filesystem"
	"github.com/arandu-io/hesape/log"
)

// Config is a bucket on any S3-compatible service.
type Config struct {
	// Endpoint is the service, without the bucket:
	// https://<account>.r2.cloudflarestorage.com
	Endpoint string
	// Bucket is the bucket name.
	Bucket string
	// Region is what the signature is computed for. R2 wants "auto".
	Region string
	// AccessKey and SecretKey come from configuration, never from a literal.
	AccessKey string
	SecretKey string
	// Client is the HTTP client. Nil installs one with the instrumented
	// transport, so every call to the bucket shows up on the request timeline
	// rather than as unaccounted time.
	Client *http.Client
	// PathStyle puts the bucket in the path rather than in the host. R2 and
	// MinIO want it; Amazon deprecated it.
	PathStyle bool
}

// R2Config is the short form for Cloudflare R2.
type R2Config struct {
	// AccountID is the Cloudflare account, which is what the endpoint is
	// derived from.
	AccountID string
	Bucket    string
	AccessKey string
	SecretKey string
	Client    *http.Client
}

// R2 returns an adapter on Cloudflare R2.
//
// The endpoint and the region are derived rather than asked for, because getting
// either wrong produces a signature error that names neither.
func R2(cfg R2Config) (*AwsS3V3Adapter, error) {
	if cfg.AccountID == "" {
		return nil, errors.New("filesystem/s3: R2 needs the account id, which is what the endpoint is built from")
	}
	return New(Config{
		Endpoint:  fmt.Sprintf("https://%s.r2.cloudflarestorage.com", cfg.AccountID),
		Bucket:    cfg.Bucket,
		Region:    "auto",
		AccessKey: cfg.AccessKey,
		SecretKey: cfg.SecretKey,
		Client:    cfg.Client,
		PathStyle: true,
	})
}

// AwsS3V3Adapter is a bucket, behind the [filesystem.Adapter] contract.
type AwsS3V3Adapter struct {
	cfg    Config
	client *http.Client
	// scheme and host are the endpoint taken apart once, because putting the
	// bucket in the host means rebuilding the URL on every call.
	scheme string
	host   string
}

// New returns an adapter on any S3-compatible service.
func New(cfg Config) (*AwsS3V3Adapter, error) {
	switch {
	case cfg.Endpoint == "":
		return nil, errors.New("filesystem/s3: no endpoint")
	case cfg.Bucket == "":
		return nil, errors.New("filesystem/s3: no bucket")
	case cfg.AccessKey == "" || cfg.SecretKey == "":
		return nil, errors.New("filesystem/s3: no credentials")
	}
	if cfg.Region == "" {
		cfg.Region = "auto"
	}
	cfg.Endpoint = strings.TrimSuffix(cfg.Endpoint, "/")

	parsed, err := url.Parse(cfg.Endpoint)
	if err != nil || parsed.Host == "" {
		return nil, fmt.Errorf("filesystem/s3: %q is not an endpoint: it needs a scheme and a host", cfg.Endpoint)
	}

	client := cfg.Client
	if client == nil {
		// The instrumented transport, so a slow bucket shows up on the timeline
		// as an outbound call rather than as "other".
		client = log.Client(30 * time.Second)
	}
	return &AwsS3V3Adapter{cfg: cfg, client: client, scheme: parsed.Scheme, host: parsed.Host}, nil
}

var (
	_ filesystem.Adapter   = (*AwsS3V3Adapter)(nil)
	_ filesystem.Presigner = (*AwsS3V3Adapter)(nil)
)

// Put uploads a file.
func (a *AwsS3V3Adapter) Put(ctx context.Context, storedPath string, body io.Reader, contentType string) error {
	// Buffered because SigV4 signs the payload hash, and a hash needs the whole
	// body. Streaming uploads need the chunked signing variant, which is another
	// four hundred lines for a case this package does not have yet -- a file that
	// does not fit in memory is a multipart upload, and that is the trigger to
	// write it.
	payload, err := io.ReadAll(body)
	if err != nil {
		return fmt.Errorf("filesystem/s3: reading the body of %s: %w", storedPath, err)
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	req, err := a.request(ctx, http.MethodPut, storedPath, payload)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", contentType)
	a.sign(req, payload)

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("filesystem/s3: uploading %s: %w", storedPath, err)
	}
	defer drain(resp)

	if resp.StatusCode >= 300 {
		return fmt.Errorf("filesystem/s3: uploading %s: %s", storedPath, describe(resp))
	}
	return nil
}

// Get downloads a file. The caller closes File.Body.
func (a *AwsS3V3Adapter) Get(ctx context.Context, storedPath string) (filesystem.File, error) {
	req, err := a.request(ctx, http.MethodGet, storedPath, nil)
	if err != nil {
		return filesystem.File{}, err
	}
	a.sign(req, nil)

	resp, err := a.client.Do(req)
	if err != nil {
		return filesystem.File{}, fmt.Errorf("filesystem/s3: downloading %s: %w", storedPath, err)
	}
	if absent(resp.StatusCode) {
		drain(resp)
		return filesystem.File{}, filesystem.ErrNotFound
	}
	if resp.StatusCode >= 300 {
		err := fmt.Errorf("filesystem/s3: downloading %s: %s", storedPath, describe(resp))
		drain(resp)
		return filesystem.File{}, err
	}

	return filesystem.File{Info: info(storedPath, resp), Body: resp.Body}, nil
}

// Stat answers what Get would carry, without moving the bytes.
func (a *AwsS3V3Adapter) Stat(ctx context.Context, storedPath string) (filesystem.Info, error) {
	resp, err := a.head(ctx, storedPath)
	if err != nil {
		return filesystem.Info{}, err
	}
	defer drain(resp)

	if absent(resp.StatusCode) {
		return filesystem.Info{}, filesystem.ErrNotFound
	}
	if resp.StatusCode >= 300 {
		return filesystem.Info{}, fmt.Errorf("filesystem/s3: reading %s: %s", storedPath, describe(resp))
	}
	return info(storedPath, resp), nil
}

// Exists reports whether the path is there.
func (a *AwsS3V3Adapter) Exists(ctx context.Context, storedPath string) (bool, error) {
	resp, err := a.head(ctx, storedPath)
	if err != nil {
		return false, err
	}
	defer drain(resp)

	switch {
	case resp.StatusCode == http.StatusOK:
		return true, nil
	case absent(resp.StatusCode):
		return false, nil
	default:
		return false, fmt.Errorf("filesystem/s3: checking %s: %s", storedPath, describe(resp))
	}
}

// Delete removes a file. Removing what is not there is not an error.
func (a *AwsS3V3Adapter) Delete(ctx context.Context, storedPath string) error {
	req, err := a.request(ctx, http.MethodDelete, storedPath, nil)
	if err != nil {
		return err
	}
	a.sign(req, nil)

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("filesystem/s3: deleting %s: %w", storedPath, err)
	}
	defer drain(resp)

	if resp.StatusCode >= 300 && resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("filesystem/s3: deleting %s: %s", storedPath, describe(resp))
	}
	return nil
}

// listResult is what the bucket answers to a list, in the v2 shape.
type listResult struct {
	Contents []struct {
		Key string `xml:"Key"`
	} `xml:"Contents"`
	IsTruncated           bool   `xml:"IsTruncated"`
	NextContinuationToken string `xml:"NextContinuationToken"`
}

// List returns the stored paths under a prefix.
//
// The prefix already carries the tenant, and it is passed to the bucket rather
// than filtered afterwards: a listing that fetched everything and dropped what
// did not match would page through another customer's object names to do it.
func (a *AwsS3V3Adapter) List(ctx context.Context, prefix string) ([]string, error) {
	var out []string
	token := ""

	for {
		query := url.Values{}
		query.Set("list-type", "2")
		query.Set("prefix", prefix)
		if token != "" {
			query.Set("continuation-token", token)
		}

		req, err := a.request(ctx, http.MethodGet, "", nil)
		if err != nil {
			return nil, err
		}
		req.URL.RawQuery = canonicalQuery(query)
		a.sign(req, nil)

		resp, err := a.client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("filesystem/s3: listing %s: %w", prefix, err)
		}
		if resp.StatusCode >= 300 {
			err := fmt.Errorf("filesystem/s3: listing %s: %s", prefix, describe(resp))
			drain(resp)
			return nil, err
		}

		var result listResult
		decodeErr := xml.NewDecoder(resp.Body).Decode(&result)
		drain(resp)
		if decodeErr != nil {
			return nil, fmt.Errorf("filesystem/s3: reading the listing: %w", decodeErr)
		}

		for _, item := range result.Contents {
			// A key ending in a slash is a directory marker some tools write. It
			// is not an object anybody stored here, and returning it would put a
			// key in a listing that Get answers ErrNotFound for.
			if strings.HasSuffix(item.Key, "/") {
				continue
			}
			out = append(out, item.Key)
		}

		// A bucket with more than a thousand objects answers in pages, and
		// stopping at the first one silently loses the rest.
		if !result.IsTruncated || result.NextContinuationToken == "" {
			return out, nil
		}
		token = result.NextContinuationToken
	}
}

// presignMaxTTL is the longest life SigV4 query authorization allows.
const presignMaxTTL = 7 * 24 * time.Hour

// PresignGet returns a URL that serves one object for ttl and then stops.
//
// This is the half of [filesystem.URLSigner.TemporaryURL] that an object store
// can do and a directory cannot: the link is signed here and redeemed by the
// bucket, so the bytes never pass through the application. Nothing is called
// over the network to make one.
//
// It is not a way around authorization. The path handed in was built by
// [filesystem.Key], which means a Policy already ran and named the tenant; the
// signature carries that decision for as long as it is worth carrying, which is
// why a lifetime is required and why seven days is the ceiling the protocol
// itself sets.
func (a *AwsS3V3Adapter) PresignGet(_ context.Context, storedPath string, ttl time.Duration) (string, error) {
	switch {
	case ttl <= 0:
		return "", fmt.Errorf("filesystem/s3: a presigned URL needs a lifetime, and %s is not one", ttl)
	case ttl > presignMaxTTL:
		return "", fmt.Errorf("filesystem/s3: %s is longer than the seven days SigV4 allows a query signature to live", ttl)
	}

	now := time.Now().UTC()
	stamp := now.Format("20060102T150405Z")
	date := now.Format("20060102")
	base, path := a.url(storedPath)

	query := url.Values{}
	query.Set("X-Amz-Algorithm", "AWS4-HMAC-SHA256")
	query.Set("X-Amz-Credential", a.cfg.AccessKey+"/"+scope(date, a.cfg.Region))
	query.Set("X-Amz-Date", stamp)
	query.Set("X-Amz-Expires", strconv.Itoa(int(ttl.Seconds())))
	query.Set("X-Amz-SignedHeaders", "host")
	canonicalQ := canonicalQuery(query)

	// The host is the only signed header, so a browser following the link sends
	// nothing that has to match. "UNSIGNED-PAYLOAD" is what a query signature
	// signs instead of a body hash, because a GET has no body to hash.
	host := a.hostFor()
	canonicalRequest := strings.Join([]string{
		http.MethodGet,
		path,
		canonicalQ,
		"host:" + host + "\n",
		"host",
		"UNSIGNED-PAYLOAD",
	}, "\n")

	signature := a.signature(date, stamp, canonicalRequest)
	return base + path + "?" + canonicalQ + "&X-Amz-Signature=" + signature, nil
}

// url returns the origin and the escaped path for a stored path.
func (a *AwsS3V3Adapter) url(storedPath string) (base, path string) {
	if a.cfg.PathStyle {
		path = "/" + a.cfg.Bucket + "/"
	} else {
		path = "/"
	}
	if storedPath != "" {
		path += escape(storedPath, true)
	}
	return a.scheme + "://" + a.hostFor(), path
}

// hostFor is the host the request goes to, which carries the bucket unless the
// bucket is in the path.
func (a *AwsS3V3Adapter) hostFor() string {
	if a.cfg.PathStyle {
		return a.host
	}
	return a.cfg.Bucket + "." + a.host
}

// request builds the URL for a stored path.
func (a *AwsS3V3Adapter) request(ctx context.Context, method, storedPath string, body []byte) (*http.Request, error) {
	base, path := a.url(storedPath)

	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, reader)
	if err != nil {
		return nil, fmt.Errorf("filesystem/s3: building the request: %w", err)
	}
	if body != nil {
		req.ContentLength = int64(len(body))
	}
	return req, nil
}

func (a *AwsS3V3Adapter) head(ctx context.Context, storedPath string) (*http.Response, error) {
	req, err := a.request(ctx, http.MethodHead, storedPath, nil)
	if err != nil {
		return nil, err
	}
	a.sign(req, nil)

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("filesystem/s3: reading %s: %w", storedPath, err)
	}
	return resp, nil
}

// info reads the metadata off a response.
func info(storedPath string, resp *http.Response) filesystem.Info {
	modified, _ := http.ParseTime(resp.Header.Get("Last-Modified"))
	size := resp.ContentLength
	if size < 0 {
		// A chunked response has no length, and a negative one written into a
		// Content-Length on the way back out is a broken download.
		size = 0
	}
	return filesystem.Info{
		Key:         storedPath,
		Size:        size,
		ContentType: resp.Header.Get("Content-Type"),
		ModifiedAt:  modified,
	}
}

// absent reports the two statuses that both mean "not for you".
//
// 403 is also not-found: a bucket policy that hides an object answers forbidden,
// and telling the two apart would leak which keys exist.
func absent(status int) bool {
	return status == http.StatusNotFound || status == http.StatusForbidden
}

func drain(resp *http.Response) {
	// Read what is left before closing, or the connection cannot be reused and
	// every call opens a new one.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	_ = resp.Body.Close()
}

// describe turns an error response into something readable.
//
// S3 answers XML with a Code and a Message, and a bare "403 Forbidden" sends
// people to check credentials when the actual problem is a bucket name.
func describe(resp *http.Response) string {
	var body struct {
		Code    string `xml:"Code"`
		Message string `xml:"Message"`
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err := xml.Unmarshal(raw, &body); err == nil && body.Code != "" {
		return fmt.Sprintf("%s (%s: %s)", resp.Status, body.Code, body.Message)
	}
	return resp.Status
}

// --- SigV4 ---

// sign adds the AWS Signature Version 4 headers.
//
// Two hundred lines against an SDK that brings a hundred modules, its own
// credential chain and its own retry policy. The algorithm has not changed since
// 2012 and is fully specified; the SDK's surface changes every quarter.
func (a *AwsS3V3Adapter) sign(req *http.Request, payload []byte) {
	now := time.Now().UTC()
	stamp := now.Format("20060102T150405Z")
	date := now.Format("20060102")

	sum := sha256.Sum256(payload)
	hashed := hex.EncodeToString(sum[:])

	req.Header.Set("Host", req.URL.Host)
	req.Header.Set("X-Amz-Date", stamp)
	req.Header.Set("X-Amz-Content-Sha256", hashed)

	signed, canonicalHeaders := canonicalize(req)
	canonicalRequest := strings.Join([]string{
		req.Method,
		req.URL.EscapedPath(),
		req.URL.RawQuery,
		canonicalHeaders,
		signed,
		hashed,
	}, "\n")

	req.Header.Set("Authorization", fmt.Sprintf(
		"AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		a.cfg.AccessKey, scope(date, a.cfg.Region), signed,
		a.signature(date, stamp, canonicalRequest)))
}

// signature is the last three steps of SigV4, shared by the header form and the
// query form: hash the canonical request, sign the string that names it, and
// derive the key from the secret one HMAC at a time.
func (a *AwsS3V3Adapter) signature(date, stamp, canonicalRequest string) string {
	requestSum := sha256.Sum256([]byte(canonicalRequest))
	toSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		stamp,
		scope(date, a.cfg.Region),
		hex.EncodeToString(requestSum[:]),
	}, "\n")

	key := hmacSHA256([]byte("AWS4"+a.cfg.SecretKey), date)
	key = hmacSHA256(key, a.cfg.Region)
	key = hmacSHA256(key, "s3")
	key = hmacSHA256(key, "aws4_request")
	return hex.EncodeToString(hmacSHA256(key, toSign))
}

func scope(date, region string) string {
	return strings.Join([]string{date, region, "s3", "aws4_request"}, "/")
}

// canonicalize returns the signed header list and the canonical header block.
func canonicalize(req *http.Request) (signed, canonical string) {
	names := make([]string, 0, len(req.Header)+1)
	values := map[string]string{}

	for name, v := range req.Header {
		lower := strings.ToLower(name)
		names = append(names, lower)
		values[lower] = strings.TrimSpace(strings.Join(v, ","))
	}
	if _, has := values["host"]; !has {
		names = append(names, "host")
		values["host"] = req.URL.Host
	}
	sort.Strings(names)

	var b strings.Builder
	for _, name := range names {
		b.WriteString(name)
		b.WriteString(":")
		b.WriteString(values[name])
		b.WriteString("\n")
	}
	return strings.Join(names, ";"), b.String()
}

// canonicalQuery sorts and encodes a query the way the signature expects.
//
// It is not url.Values.Encode, and the difference is not cosmetic: Encode writes
// a space as "+", the bucket re-canonicalizes it as "%20" before checking the
// signature, and the two do not match. A listing whose prefix contains a space
// is a folder somebody named "Q1 reports", so the failure is not exotic -- it is
// a 403 nobody can explain. The same string is put on the wire and into the
// canonical request, so there is one encoding and not two.
func canonicalQuery(values url.Values) string {
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		v := append([]string(nil), values[k]...)
		sort.Strings(v)
		for _, one := range v {
			parts = append(parts, escape(k, false)+"="+escape(one, false))
		}
	}
	return strings.Join(parts, "&")
}

// escape percent-encodes for SigV4: everything except the four unreserved
// punctuation marks and the alphanumerics, in uppercase hex.
//
// url.PathEscape and url.QueryEscape both leave characters AWS expects encoded
// -- "+" and "=" among them -- and a key with a plus sign in it would sign one
// way and arrive another. keepSlash is what makes a nested key stay nested
// instead of becoming one long name.
func escape(s string, keepSlash bool) string {
	const upperhex = "0123456789ABCDEF"
	var b strings.Builder
	b.Grow(len(s))
	for i := range len(s) {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9',
			c == '-', c == '_', c == '.', c == '~':
			b.WriteByte(c)
		case c == '/' && keepSlash:
			b.WriteByte(c)
		default:
			b.WriteByte('%')
			b.WriteByte(upperhex[c>>4])
			b.WriteByte(upperhex[c&15])
		}
	}
	return b.String()
}

func hmacSHA256(key []byte, data string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(data))
	return h.Sum(nil)
}
