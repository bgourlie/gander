// Package release reads Goose's releases from GitHub: the release list, each asset's
// checksum, and the archives themselves. It also finds and fetches gander's own
// releases for "gander self update".
package release

import (
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/bgourlie/gander/internal/platform"
	"github.com/bgourlie/gander/internal/version"
)

// Where Goose's releases live.
const (
	DefaultAPI      = "https://api.github.com/repos/aardappel/goose"
	DefaultReleases = "https://github.com/aardappel/goose/releases"
)

// GanderReleases is where gander's own releases live. Each has a bare binary per
// platform, named by GanderAsset, and a SHA256SUMS file.
const GanderReleases = "https://github.com/bgourlie/gander/releases"

// GanderAsset is the name of gander's own binary for t in a release. install.sh and
// "gander self update" both build it, so it never changes.
func GanderAsset(t platform.Target) string {
	name := "gander-" + t.String()
	if t.OS == "windows" {
		name += ".exe"
	}
	return name
}

// Client talks to GitHub. The zero value isn't usable; see NewClient.
type Client struct {
	HTTP      *http.Client
	API       string // the repository's API URL, such as DefaultAPI
	Releases  string // the repository's releases page; downloads are under <Releases>/download
	UserAgent string
	// Token, if set, goes with every request to API, and nowhere else.
	Token string

	// StallTimeout aborts a download attempt that receives nothing for this long.
	StallTimeout time.Duration
	// Retries is how many times a download is retried after a network error.
	Retries int
	// Backoff is the wait before the first retry; it doubles after each.
	Backoff time.Duration
}

// NewClient returns a client for Goose's releases on github.com. Without a token,
// GitHub's API allows 60 requests an hour per IP address, which runners that share
// addresses, such as GitHub's own macOS runners, use up; so GITHUB_TOKEN is sent when
// it's set, the name GitHub Actions and most tools use.
func NewClient(userAgent string) *Client {
	return &Client{
		HTTP:         &http.Client{Transport: newTransport()},
		API:          DefaultAPI,
		Releases:     DefaultReleases,
		UserAgent:    userAgent,
		Token:        os.Getenv("GITHUB_TOKEN"),
		StallTimeout: 180 * time.Second,
		Retries:      4,
		Backoff:      time.Second,
	}
}

// Only http.DefaultTransport sets Proxy: ProxyFromEnvironment; a new http.Transport{}
// would ignore HTTPS_PROXY. http.Client.Timeout would cap the whole body, which a slow
// link can't meet, so downloads use these per-phase timeouts plus a stall timer.
func newTransport() *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.DialContext = (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	t.TLSHandshakeTimeout = 30 * time.Second
	t.ResponseHeaderTimeout = 60 * time.Second
	return t
}

// Release is one GitHub release.
type Release struct {
	Tag        string  `json:"tag_name"`
	Draft      bool    `json:"draft"`
	Prerelease bool    `json:"prerelease"`
	Assets     []Asset `json:"assets"`
}

// Asset is one file attached to a release.
type Asset struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	Digest string `json:"digest"` // "sha256:<hex>", for assets uploaded since June 2025
}

// Asset returns the asset with this name.
func (r *Release) Asset(name string) (Asset, bool) {
	for _, a := range r.Assets {
		if a.Name == name {
			return a, true
		}
	}
	return Asset{}, false
}

// AssetName is the archive Goose publishes for tag on target.
func AssetName(v version.Version, t platform.Target) string {
	return "goose-" + v.String() + "-" + t.String() + t.Archive()
}

// DownloadURL is where an asset of release v is downloaded from.
func (c *Client) DownloadURL(v version.Version, name string) string {
	return c.Releases + "/download/" + v.String() + "/" + name
}

// NotFoundError means Goose has no release with this tag.
type NotFoundError struct{ Version version.Version }

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("Goose has no release %s; run \"gander list --available\" to see the releases", e.Version)
}

// RateLimitError means GitHub's API refused because its rate limit is used up.
type RateLimitError struct {
	Reset     time.Time
	WithToken bool // the request carried GITHUB_TOKEN
}

func (e *RateLimitError) Error() string {
	whose, when, then := "this network", "", "try again later"
	if e.WithToken {
		whose = "the token in GITHUB_TOKEN"
	}
	if !e.Reset.IsZero() {
		when, then = " until "+e.Reset.Local().Format("15:04"), "try again then"
	}
	msg := "GitHub's API rate limit for " + whose + " is used up" + when + "; " + then
	if !e.WithToken {
		msg += ", or set GITHUB_TOKEN to a GitHub token, which raises the limit"
	}
	return msg
}

// List returns every release, newest first.
func (c *Client) List(ctx context.Context) ([]Release, error) {
	var all []Release
	const perPage = 100
	for page := 1; page <= 50; page++ {
		var rs []Release
		if err := c.apiGet(ctx, fmt.Sprintf("/releases?per_page=%d&page=%d", perPage, page), &rs); err != nil {
			return nil, err
		}
		all = append(all, rs...)
		if len(rs) < perPage {
			break
		}
	}
	return all, nil
}

// Get returns the release tagged v.
func (c *Client) Get(ctx context.Context, v version.Version) (*Release, error) {
	var r Release
	err := c.apiGet(ctx, "/releases/tags/"+v.String(), &r)
	if errors.Is(err, errNotFound) {
		return nil, &NotFoundError{Version: v}
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// Latest is the newest release that isn't a draft, isn't marked a prerelease and has no
// suffix. GitHub's own "latest" is the newest by creation date, so a backport would
// win; this compares versions.
func Latest(rs []Release) (version.Version, bool) {
	var best version.Version
	for _, r := range rs {
		if r.Draft || r.Prerelease {
			continue
		}
		v, err := version.Parse(r.Tag)
		if err != nil || v.Suffix() != "" || v.String() != r.Tag {
			continue
		}
		if best.IsZero() || version.Compare(v, best) > 0 {
			best = v
		}
	}
	return best, !best.IsZero()
}

// LatestVersion returns the latest release.
func (c *Client) LatestVersion(ctx context.Context) (version.Version, error) {
	rs, err := c.List(ctx)
	if err != nil {
		return version.Version{}, err
	}
	v, ok := Latest(rs)
	if !ok {
		return version.Version{}, errors.New("Goose has no stable release yet; name a version to install, such as a release candidate from \"gander list --available\"")
	}
	return v, nil
}

var errNotFound = errors.New("not found")

func (c *Client) apiGet(ctx context.Context, p string, into any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.API+p, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", c.UserAgent)
	// Go's client drops it if the API ever redirects to another host.
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	resp, err := c.HTTP.Do(req.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("can't reach GitHub's API (%w); check your network connection, and HTTPS_PROXY if you use a proxy", err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusOK:
	case resp.StatusCode == http.StatusNotFound:
		return errNotFound
	case resp.StatusCode == http.StatusUnauthorized && c.Token != "":
		return errors.New("GitHub's API refused the token in GITHUB_TOKEN as bad credentials; replace it, or unset GITHUB_TOKEN")
	case rateLimited(resp):
		e := &RateLimitError{WithToken: c.Token != ""}
		if s, err := strconv.ParseInt(resp.Header.Get("X-RateLimit-Reset"), 10, 64); err == nil {
			e.Reset = time.Unix(s, 0)
		}
		return e
	default:
		return fmt.Errorf("GitHub's API answered %s for %s; try again later", resp.Status, req.URL)
	}
	if err := json.NewDecoder(resp.Body).Decode(into); err != nil {
		return fmt.Errorf("can't read GitHub's answer for %s: %w", req.URL, err)
	}
	return nil
}

func rateLimited(resp *http.Response) bool {
	if resp.StatusCode == http.StatusTooManyRequests {
		return true
	}
	return resp.StatusCode == http.StatusForbidden &&
		(resp.Header.Get("X-RateLimit-Remaining") == "0" || resp.Header.Get("Retry-After") != "")
}

// SumsName is the checksum file a release may carry.
const SumsName = "SHA256SUMS"

// Checksum returns the expected SHA-256 of asset name in release v, and where it came
// from: the release's SHA256SUMS when it has one, else the API's digest. r may be nil
// when the API couldn't be asked; then only SHA256SUMS can be used, and apiErr says why
// the API failed.
func (c *Client) Checksum(ctx context.Context, v version.Version, r *Release, name string, apiErr error) (sum []byte, from string, err error) {
	hasSums := r == nil
	if r != nil {
		_, hasSums = r.Asset(SumsName)
	}
	if hasSums {
		url := c.DownloadURL(v, SumsName)
		data, err := c.fetchSmall(ctx, url)
		switch {
		case err == nil:
			sum, err := parseSums(data, name)
			if err != nil {
				return nil, "", fmt.Errorf("%s: %w", url, err)
			}
			return sum, url, nil
		case !errors.Is(err, errNotFound):
			return nil, "", err
		case r != nil:
			return nil, "", fmt.Errorf("release %s lists %s, but %s isn't there; try again later", v, SumsName, url)
		}
	}
	if r == nil {
		return nil, "", fmt.Errorf("can't verify %s: release %s has no %s, and GitHub's API couldn't be asked for its digest: %w", name, v, SumsName, apiErr)
	}
	a, _ := r.Asset(name)
	hexSum, ok := strings.CutPrefix(a.Digest, "sha256:")
	if !ok {
		return nil, "", fmt.Errorf("can't verify %s: release %s has no %s and GitHub has no digest for it; report this at https://github.com/aardappel/goose/issues", name, v, SumsName)
	}
	sum, err = hex.DecodeString(hexSum)
	if err != nil || len(sum) != 32 {
		return nil, "", fmt.Errorf("GitHub's digest for %s, %q, isn't a SHA-256", name, a.Digest)
	}
	return sum, "GitHub's digest for " + name, nil
}

// LatestTag returns the tag of the latest release, read from where <Releases>/latest
// redirects. That's github.com, not the API, so it doesn't count against the API's
// limit of 60 requests an hour (mise#14464). GitHub's latest is the newest release by
// date that isn't a draft or prerelease, which suits gander's own releases.
func (c *Client) LatestTag(ctx context.Context) (version.Version, error) {
	url := c.Releases + "/latest"
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return version.Version{}, err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	hc := *c.HTTP
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := hc.Do(req)
	if err != nil {
		return version.Version{}, fmt.Errorf("can't reach %s (%w); check your network connection, and HTTPS_PROXY if you use a proxy", url, err)
	}
	resp.Body.Close()
	loc := resp.Header.Get("Location")
	i := strings.LastIndex(loc, "/releases/tag/")
	switch {
	case resp.StatusCode/100 != 3:
		return version.Version{}, fmt.Errorf("%s answered %s; try again later", url, resp.Status)
	case i < 0:
		// With no release, GitHub redirects to the releases page.
		return version.Version{}, fmt.Errorf("there's no published release at %s yet", c.Releases)
	}
	tag := loc[i+len("/releases/tag/"):]
	v, err := version.Parse(tag)
	if err != nil || v.String() != tag {
		return version.Version{}, fmt.Errorf("the latest release at %s has the tag %q, which isn't a version", url, tag)
	}
	return v, nil
}

// SumFor returns the SHA-256 that release v's SHA256SUMS lists for name.
func (c *Client) SumFor(ctx context.Context, v version.Version, name string) ([]byte, error) {
	url := c.DownloadURL(v, SumsName)
	data, err := c.fetchSmall(ctx, url)
	if errors.Is(err, errNotFound) {
		return nil, fmt.Errorf("release %s has no %s at %s", v, SumsName, url)
	}
	if err != nil {
		return nil, err
	}
	sum, err := parseSums(data, name)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", url, err)
	}
	return sum, nil
}

// parseSums finds name in a SHA256SUMS file. Lines read "<hash>  <name>" or, in binary
// mode, "<hash> *<name>" (mise#3112), and the name may start with "./".
func parseSums(data []byte, name string) ([]byte, error) {
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		hexSum, rest, ok := strings.Cut(strings.TrimSpace(sc.Text()), " ")
		if !ok {
			continue
		}
		rest = strings.TrimPrefix(strings.TrimLeft(rest, " "), "*")
		if strings.TrimPrefix(rest, "./") != name {
			continue
		}
		sum, err := hex.DecodeString(hexSum)
		if err != nil || len(sum) != 32 {
			return nil, fmt.Errorf("the hash for %s, %q, isn't a SHA-256", name, hexSum)
		}
		return sum, nil
	}
	return nil, fmt.Errorf("has no line for %s", name)
}

func (c *Client) fetchSmall(ctx context.Context, url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("can't download %s (%w); check your network connection, and HTTPS_PROXY if you use a proxy", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, errNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("downloading %s: %s", url, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}

// Builds lists the platforms a release has archives for, for error messages.
func (r *Release) Builds(v version.Version) []string {
	var out []string
	for _, t := range platform.Targets {
		if _, ok := r.Asset(AssetName(v, t)); ok {
			out = append(out, t.String())
		}
	}
	slices.Sort(out)
	return out
}
