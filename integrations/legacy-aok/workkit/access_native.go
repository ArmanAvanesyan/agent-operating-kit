package workkit

import (
	"context"
	"errors"
	a "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/accessv1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/sdk"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// Native drivers authenticate read requests with an explicit token and isolated
// externally enrolled profile. They never log in to, refresh, switch or remove
// the user's global CLI account. Native cache mutation is deliberately absent.
func nativeTransport(ctx context.Context, c a.Configuration, token string, check func() error, fallback http.RoundTripper) http.RoundTripper {
	return nativeRoundTrip(func(req *http.Request) (*http.Response, error) {
		if e := sdk.VerifyNativeProfile(ctx, c); e != nil {
			return nil, e
		}
		if e := check(); e != nil {
			return nil, e
		}
		root := c.Native.ProfileRoot
		env := []string{"HOME=" + root, "USERPROFILE=" + root, "XDG_CONFIG_HOME=" + root, "DO_NOT_TRACK=1"}
		args := []string{}
		switch c.Mode {
		case "gh-native":
			u, _ := url.Parse(c.Origin)
			host := u.Host
			if host == "api.github.com" {
				host = "github.com"
			}
			key := githubTokenKey(host)
			env = append(env, "GH_CONFIG_DIR="+root, "GH_PROMPT_DISABLED=1", "GH_TELEMETRY=0", key+"="+token)
			args = []string{"api", "--hostname", host, "--method", "GET", "/user"}
		case "az-native":
			if len(c.Tool.Extensions) != 1 || c.Tool.Extensions[0].Layout != "tree-v1" {
				return nil, errors.New("exact Azure extension tree required")
			}
			if st, e := os.Stat(filepath.Join(c.Tool.Extensions[0].Path, "azure-devops")); e != nil || !st.IsDir() {
				return nil, errors.New("Azure extension root must contain azure-devops directory")
			}
			env = append(env, "PYTHONDONTWRITEBYTECODE=1", "PYTHONNOUSERSITE=1", "AZURE_CORE_COLLECT_TELEMETRY=0", "AZURE_CONFIG_DIR="+root, "AZURE_EXTENSION_DIR="+c.Tool.Extensions[0].Path, "AZURE_EXTENSION_USE_DYNAMIC_INSTALL=no", "AZURE_DEVOPS_EXT_PAT="+token)
			args = []string{"devops", "invoke", "--organization", c.Origin + "/" + url.PathEscape(c.ProviderLookup), "--area", "location", "--resource", "connectionData", "--http-method", "GET", "--api-version", "7.1", "--detect", "false", "--output", "json"}
		case "acli-native":
			env = append(env, "ACLI_CONFIG_DIR="+root)
			args = []string{"jira", "auth", "status"}
		default:
			return nil, errors.New("unknown native auth driver")
		}
		raw, e := sdk.NativeToolRun(ctx, *c.Tool, args, nil, env, root, check)
		if e != nil {
			return nil, e
		}
		if c.Mode == "acli-native" {
			if c.Native.CredentialMode != "jira-basic" || sdk.SHA256(raw) != c.Native.StatusSHA256 {
				return nil, errors.New("enrolled acli status changed; hybrid identity unknown")
			}

			if e = check(); e != nil {
				return nil, e
			}
			return fallback.RoundTrip(req)
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(raw))), Request: req}, nil
	})
}

type nativeRoundTrip func(*http.Request) (*http.Response, error)

func (f nativeRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
