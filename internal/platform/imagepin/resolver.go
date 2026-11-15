// Package imagepin resolves approved OCI registry tags into immutable digests.
package imagepin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const maxManifest = 4 * 1024 * 1024

var digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
var repositoryPattern = regexp.MustCompile(`^[a-z0-9]+(?:[._-]+[a-z0-9]+)*(?:/[a-z0-9]+(?:[._-]+[a-z0-9]+)*)*$`)
var tagPattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$`)

type Resolver struct {
	AllowedRegistries map[string]bool
	PrivateRegistries map[string]bool
	Client            *http.Client
}

// New permits public registries by default. Private registry network access is
// an installation decision, never a field accepted from an API caller.
func New(approved, private []string) *Resolver {
	r := &Resolver{AllowedRegistries: map[string]bool{"registry-1.docker.io": true, "ghcr.io": true}, PrivateRegistries: make(map[string]bool)}
	for _, host := range approved {
		if host = strings.TrimSpace(strings.ToLower(host)); host != "" {
			r.AllowedRegistries[host] = true
		}
	}
	for _, host := range private {
		if host = strings.TrimSpace(strings.ToLower(host)); host != "" {
			r.PrivateRegistries[host] = true
			r.AllowedRegistries[host] = true
		}
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, err
		}
		var last error
		for _, address := range addresses {
			ip := address.IP
			if ip.IsLoopback() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || (!r.PrivateRegistries[net.JoinHostPort(host, port)] && !r.PrivateRegistries[host] && ip.IsPrivate()) {
				last = fmt.Errorf("registry address is outside the approved network")
				continue
			}
			var d net.Dialer
			d.Timeout = 3 * time.Second
			conn, err := d.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if err == nil {
				return conn, nil
			}
			last = err
		}
		if last == nil {
			last = fmt.Errorf("registry has no approved address")
		}
		return nil, last
	}
	r.Client = &http.Client{Transport: transport, Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return r
}

func splitReference(image string) (host, repository, reference string, pinned bool, err error) {
	if image == "" || strings.TrimSpace(image) != image || strings.ContainsAny(image, " \\?#") || strings.Contains(image, "://") {
		err = fmt.Errorf("use an OCI image reference without a URL or credentials")
		return
	}
	name := image
	if i := strings.IndexByte(name, '@'); i >= 0 {
		reference = name[i+1:]
		name = name[:i]
		pinned = true
		if !digestPattern.MatchString(reference) {
			err = fmt.Errorf("use a full sha256 image digest")
			return
		}
	}
	lastSlash := strings.LastIndexByte(name, '/')
	if colon := strings.LastIndexByte(name, ':'); colon > lastSlash {
		if !pinned {
			reference = name[colon+1:]
		}
		name = name[:colon]
	}
	first, rest, found := strings.Cut(name, "/")
	if found && (strings.ContainsAny(first, ".:") || first == "localhost") {
		host = strings.ToLower(first)
		repository = rest
	} else {
		host = "registry-1.docker.io"
		repository = name
	}
	if host == "docker.io" || host == "index.docker.io" {
		host = "registry-1.docker.io"
	}
	if host == "registry-1.docker.io" && !strings.Contains(repository, "/") {
		repository = "library/" + repository
	}
	if !repositoryPattern.MatchString(repository) || strings.ContainsAny(host, "@/#?\\ ") {
		err = fmt.Errorf("invalid registry or repository name")
		return
	}
	if !pinned {
		if reference == "" {
			reference = "latest"
		}
		if !tagPattern.MatchString(reference) {
			err = fmt.Errorf("invalid image tag")
		}
	}
	return
}

func (r *Resolver) Pin(ctx context.Context, image string) (string, error) {
	host, repository, ref, pinned, err := splitReference(image)
	if err != nil {
		return "", err
	}
	outputHost := host
	if host == "registry-1.docker.io" {
		outputHost = "docker.io"
	}
	if !r.AllowedRegistries[host] {
		return "", fmt.Errorf("this registry is not approved for bounded execution; ask the installation administrator")
	}
	if pinned {
		return outputHost + "/" + repository + "@" + ref, nil
	}
	endpoint := "https://" + host + "/v2/" + repository + "/manifests/" + url.PathEscape(ref)
	response, err := r.manifest(ctx, endpoint, "")
	if err != nil {
		return "", err
	}
	if response.StatusCode == http.StatusUnauthorized {
		challenge := response.Header.Get("WWW-Authenticate")
		_ = response.Body.Close()
		token, err := r.token(ctx, host, repository, challenge)
		if err != nil {
			return "", err
		}
		response, err = r.manifest(ctx, endpoint, token)
		if err != nil {
			return "", err
		}
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("registry could not resolve this image tag (HTTP %d); no configuration was saved", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxManifest+1))
	if err != nil {
		return "", err
	}
	if len(body) > maxManifest {
		return "", fmt.Errorf("registry manifest exceeds its read budget")
	}
	var manifest struct {
		SchemaVersion int    `json:"schemaVersion"`
		MediaType     string `json:"mediaType"`
	}
	if json.Unmarshal(body, &manifest) != nil || manifest.SchemaVersion != 2 {
		return "", fmt.Errorf("registry returned an unsupported image manifest")
	}
	sum := sha256.Sum256(body)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	if reported := response.Header.Get("Docker-Content-Digest"); reported != "" && reported != digest {
		return "", fmt.Errorf("registry manifest digest does not match its content")
	}
	return outputHost + "/" + repository + "@" + digest, nil
}

func (r *Resolver) manifest(ctx context.Context, endpoint, token string) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/vnd.oci.image.index.v1+json, application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.docker.distribution.manifest.v2+json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	return r.Client.Do(request)
}

func bearerParameters(header string) (map[string]string, error) {
	scheme, rest, ok := strings.Cut(header, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return nil, fmt.Errorf("registry requires unsupported authentication; supply a digest")
	}
	result := make(map[string]string)
	for rest = strings.TrimSpace(rest); rest != ""; rest = strings.TrimSpace(rest) {
		key, tail, ok := strings.Cut(rest, "=")
		if !ok || key == "" || !strings.HasPrefix(tail, `"`) {
			return nil, fmt.Errorf("invalid registry authentication challenge")
		}
		key = strings.ToLower(strings.TrimSpace(key))
		tail = tail[1:]
		var value strings.Builder
		closed := false
		for len(tail) > 0 {
			c := tail[0]
			tail = tail[1:]
			if c == '"' {
				closed = true
				break
			}
			if c == '\\' {
				if len(tail) == 0 {
					break
				}
				c = tail[0]
				tail = tail[1:]
			}
			value.WriteByte(c)
		}
		if !closed || len(value.String()) > 2048 {
			return nil, fmt.Errorf("invalid registry authentication challenge")
		}
		if _, exists := result[key]; exists {
			return nil, fmt.Errorf("duplicate registry authentication parameter")
		}
		result[key] = value.String()
		rest = strings.TrimSpace(tail)
		if rest == "" {
			break
		}
		if rest[0] != ',' {
			return nil, fmt.Errorf("invalid registry authentication parameters")
		}
		rest = rest[1:]
	}
	return result, nil
}

func (r *Resolver) token(ctx context.Context, registry, repository, challenge string) (string, error) {
	parameters, err := bearerParameters(challenge)
	if err != nil {
		return "", err
	}
	realm, err := url.Parse(parameters["realm"])
	if err != nil || realm.Scheme != "https" || realm.User != nil || realm.Fragment != "" || (realm.Host != registry && (registry != "registry-1.docker.io" || realm.Host != "auth.docker.io")) {
		return "", fmt.Errorf("registry authentication endpoint is not approved")
	}
	query := realm.Query()
	query.Set("scope", "repository:"+repository+":pull")
	if parameters["service"] != "" {
		query.Set("service", parameters["service"])
	}
	realm.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, realm.String(), nil)
	if err != nil {
		return "", err
	}
	response, err := r.Client.Do(request)
	if err != nil {
		return "", err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("registry token request was refused")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 65537))
	if err != nil || len(body) > 65536 {
		return "", fmt.Errorf("registry token response exceeds its read budget")
	}
	var token struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if json.Unmarshal(body, &token) != nil {
		return "", fmt.Errorf("invalid registry token response")
	}
	if token.Token == "" {
		token.Token = token.AccessToken
	}
	if token.Token == "" || len(token.Token) > 16384 || strings.ContainsAny(token.Token, "\r\n") {
		return "", fmt.Errorf("invalid registry pull token")
	}
	return token.Token, nil
}
