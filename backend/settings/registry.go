package settings

import (
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

type Type string

const (
	TypeBool       Type = "bool"
	TypeInt        Type = "int"
	TypeString     Type = "string"
	TypeStringList Type = "string_list"
	TypeSecret     Type = "secret"
)

const (
	AppPublicURL               = "app.public_url"
	AuthRegistrationEnabled    = "auth.registration_enabled"
	AuthPasswordLoginEnabled   = "auth.password_login_enabled"
	AuthExternalAutoCreate     = "auth.external_auto_create"
	AuthExternalSessionMaxDays = "auth.external_session_max_days"
	OIDCEnabled                = "auth.oidc.enabled"
	OIDCIssuer                 = "auth.oidc.issuer"
	OIDCClientID               = "auth.oidc.client_id"
	OIDCClientSecret           = "auth.oidc.client_secret"
	OIDCScopes                 = "auth.oidc.scopes"
	OIDCButtonLabel            = "auth.oidc.button_label"
	OIDCAutoRedirect           = "auth.oidc.auto_redirect"
	OIDCUsernameClaim          = "auth.oidc.username_claim"
	OIDCGroupsClaim            = "auth.oidc.groups_claim"
	AuthAdminGroup             = "auth.admin_group"
	AuthLinkMatchUsername      = "auth.link.match_username"
	AuthLinkMatchEmail         = "auth.link.match_email"
	AuthProxyLogoutURL         = "auth.proxy.logout_url"
	MetadataMatchingPaused     = "metadata.matching_paused"
	BootstrapCompleted         = "internal.bootstrap_completed"
	InstallationID             = "internal.installation_id"
)

type Def struct {
	Key       string
	Type      Type
	Default   any
	Internal  bool
	Help      string
	normalize func(any) any
	validate  func(any) error
}

func (d Def) Secret() bool { return d.Type == TypeSecret }

var defs = []Def{
	{
		Key: AppPublicURL, Type: TypeString, Default: "",
		Help:      "Externally reachable base URL of this server. Required for OIDC.",
		normalize: normalizeURL, validate: baseURL,
	},
	{Key: AuthRegistrationEnabled, Type: TypeBool, Default: false, Help: "Allow anyone to create an account."},
	{Key: AuthPasswordLoginEnabled, Type: TypeBool, Default: true, Help: "Allow logging in with a username and password."},
	{Key: AuthExternalAutoCreate, Type: TypeBool, Default: true, Help: "Create an account on first OIDC or proxy login."},
	{
		Key: AuthExternalSessionMaxDays, Type: TypeInt, Default: 30,
		Help:     "Maximum lifetime in days of a session created by OIDC or a proxy.",
		validate: intRange(1, 3650),
	},
	{Key: OIDCEnabled, Type: TypeBool, Default: false, Help: "Enable single sign-on through an OIDC provider."},
	// The issuer must match the iss claim byte for byte, so it is not normalized.
	{Key: OIDCIssuer, Type: TypeString, Default: "", Help: "OIDC issuer URL, used for discovery.", validate: optionalURL},
	{Key: OIDCClientID, Type: TypeString, Default: "", Help: "OIDC client ID."},
	{Key: OIDCClientSecret, Type: TypeSecret, Default: "", Help: "OIDC client secret."},
	{
		Key: OIDCScopes, Type: TypeStringList, Default: []string{"openid", "profile", "email"},
		Help:     "Scopes requested from the OIDC provider.",
		validate: scopes,
	},
	{Key: OIDCButtonLabel, Type: TypeString, Default: "Sign in with SSO", Help: "Label of the login button.", validate: notBlank},
	{Key: OIDCAutoRedirect, Type: TypeBool, Default: false, Help: "Send users straight to the provider from the login page."},
	{Key: OIDCUsernameClaim, Type: TypeString, Default: "preferred_username", Help: "Claim holding the username.", validate: notBlank},
	{Key: OIDCGroupsClaim, Type: TypeString, Default: "groups", Help: "Claim holding the group list.", validate: notBlank},
	{Key: AuthAdminGroup, Type: TypeString, Default: "", Help: "Group granting admin permissions. Empty disables the mapping."},
	{
		Key: AuthLinkMatchUsername, Type: TypeBool, Default: false,
		Help: "Link an external login to the local account with the same username.",
	},
	{
		Key: AuthLinkMatchEmail, Type: TypeBool, Default: false,
		Help: "Link an external login to the local account with the same verified email. Only accounts with no password and no linked logins link automatically. Through single sign-on, an account with a password links after its password is confirmed.",
	},
	{Key: AuthProxyLogoutURL, Type: TypeString, Default: "", Help: "Where to send proxy users on logout. Empty hides the logout button.", validate: optionalURL},
	{
		Key: MetadataMatchingPaused, Type: TypeBool, Default: false,
		Help: "Pause matching series with metadata providers automatically. Linked metadata keeps refreshing.",
	},
	{Key: BootstrapCompleted, Type: TypeBool, Default: false, Internal: true},
	// Seeded by migration 011; prefixes globally unique OPDS (Atom) IDs.
	{Key: InstallationID, Type: TypeString, Default: "", Internal: true},
}

var byKey = func() map[string]Def {
	m := make(map[string]Def, len(defs))
	for _, d := range defs {
		m[d.Key] = d
	}
	return m
}()

func Lookup(key string) (Def, bool) {
	d, ok := byKey[key]
	return d, ok
}

func All() []Def { return slices.Clone(defs) }

func (d Def) Parse(v any) (any, error) {
	out, err := d.coerce(v)
	if err != nil {
		return nil, err
	}
	if d.normalize != nil {
		out = d.normalize(out)
	}
	if d.validate != nil {
		if err := d.validate(out); err != nil {
			return nil, fmt.Errorf("%s: %w", d.Key, err)
		}
	}
	return out, nil
}

func (d Def) ParseString(s string) (any, error) {
	switch d.Type {
	case TypeBool:
		b, err := strconv.ParseBool(s)
		if err != nil {
			return nil, fmt.Errorf("%s: expected a boolean", d.Key)
		}
		return d.Parse(b)
	case TypeInt:
		n, err := strconv.Atoi(s)
		if err != nil {
			return nil, fmt.Errorf("%s: expected an integer", d.Key)
		}
		return d.Parse(n)
	case TypeStringList:
		return d.Parse(strings.FieldsFunc(s, func(r rune) bool {
			return r == ',' || r == ' ' || r == '\t' || r == '\n'
		}))
	default:
		return d.Parse(s)
	}
}

func (d Def) coerce(v any) (any, error) {
	switch d.Type {
	case TypeBool:
		if b, ok := v.(bool); ok {
			return b, nil
		}
	case TypeInt:
		switch n := v.(type) {
		case int:
			return n, nil
		case int64:
			return int(n), nil
		case float64:
			if n == math.Trunc(n) {
				return int(n), nil
			}
		case json.Number:
			if i, err := n.Int64(); err == nil {
				return int(i), nil
			}
		}
	case TypeString, TypeSecret:
		if s, ok := v.(string); ok {
			return s, nil
		}
	case TypeStringList:
		switch l := v.(type) {
		case []string:
			return slices.Clone(l), nil
		case []any:
			out := make([]string, len(l))
			for i, e := range l {
				s, ok := e.(string)
				if !ok {
					return nil, fmt.Errorf("%s: expected a list of strings", d.Key)
				}
				out[i] = s
			}
			return out, nil
		}
	}
	return nil, fmt.Errorf("%s: expected a value of type %s", d.Key, d.Type)
}

func normalizeURL(v any) any {
	s := strings.TrimRight(v.(string), "/")
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return s
	}
	u.Scheme, u.Host = strings.ToLower(u.Scheme), strings.ToLower(u.Host)
	return u.String()
}

func optionalURL(v any) error {
	s := v.(string)
	if s == "" {
		return nil
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" || !slices.Contains([]string{"http", "https"}, strings.ToLower(u.Scheme)) {
		return fmt.Errorf("must be an http or https URL")
	}
	return nil
}

// baseURL is an optionalURL that paths can be appended to.
func baseURL(v any) error {
	if err := optionalURL(v); err != nil {
		return err
	}
	u, _ := url.Parse(v.(string))
	if u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return fmt.Errorf("must not contain credentials, a query or a fragment")
	}
	return nil
}

func notBlank(v any) error {
	if strings.TrimSpace(v.(string)) == "" {
		return fmt.Errorf("must not be empty")
	}
	return nil
}

func intRange(lo, hi int) func(any) error {
	return func(v any) error {
		if n := v.(int); n < lo || n > hi {
			return fmt.Errorf("must be between %d and %d", lo, hi)
		}
		return nil
	}
}

func scopes(v any) error {
	l := v.([]string)
	if !slices.Contains(l, "openid") {
		return fmt.Errorf("must include the openid scope")
	}
	for _, s := range l {
		if strings.TrimSpace(s) == "" {
			return fmt.Errorf("must not contain empty scopes")
		}
	}
	return nil
}
