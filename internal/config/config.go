package config

import (
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	ModeLocal  = "local"
	ModeHosted = "hosted"
)

type Config struct {
	DeploymentMode           string
	DataDir                  string
	APIToken                 string
	Address                  string
	DatabaseURL              string
	JWTSecret                string
	JWTIssuer                string
	JWTAudience              string
	OIDCIssuer               string
	OIDCBackchannelURL       string
	OIDCCLIClientID          string
	OIDCCLIScope             string
	ArtifactPublicKey        string
	ArtifactPublicKeys       []string
	WorkspaceID              string
	ArtifactStore            string
	S3Endpoint               string
	S3AccessKey              string
	S3SecretKey              string
	S3Bucket                 string
	S3Secure                 bool
	AllowedOrigins           []string
	TrustedProxies           []string
	RateLimitPerMinute       int
	AllowLegacyUnscoped      bool
	AllowHostedBootstrapAuth bool
	AllowInsecureHosted      bool
	TermsURL                 string
	PrivacyURL               string
	SupportURL               string
	AbuseEmail               string
	SecurityEmail            string
	ConsoleURL               string
	SMTPAddress              string
	SMTPUsername             string
	SMTPPassword             string
	SMTPFrom                 string
	SMTPTLSMode              string
	AllowUndeliveredInvites  bool
	ComplianceAdminIDs       []string
	AuditRetentionDays       int
	AuditRetentionInterval   time.Duration
}

func Load() (Config, error) {
	data := valueOrDefault("AGENTX_DATA_DIR", "./data")
	addr := valueOrDefault("AGENTX_HTTP_ADDR", ":8080")
	store := strings.ToLower(valueOrDefault("AGENTX_ARTIFACT_STORE", "file"))
	mode := strings.ToLower(valueOrDefault("AGENTX_DEPLOYMENT_MODE", ModeLocal))
	secure, err := boolValue("AGENTX_S3_SECURE", false)
	if err != nil {
		return Config{}, err
	}
	legacyDefault := mode == ModeLocal && os.Getenv("AGENTX_DATABASE_URL") == ""
	legacy, err := boolValue("AGENTX_ALLOW_LEGACY_UNSCOPED", legacyDefault)
	if err != nil {
		return Config{}, err
	}
	bootstrapAuth, err := boolValue("AGENTX_ALLOW_HOSTED_BOOTSTRAP_AUTH", false)
	if err != nil {
		return Config{}, err
	}
	insecureHosted, err := boolValue("AGENTX_ALLOW_INSECURE_HOSTED", false)
	if err != nil {
		return Config{}, err
	}
	allowUndeliveredInvites, err := boolValue("AGENTX_ALLOW_UNDELIVERED_INVITATIONS", false)
	if err != nil {
		return Config{}, err
	}
	rateLimit, err := intValue("AGENTX_RATE_LIMIT_PER_MINUTE", 600)
	if err != nil {
		return Config{}, err
	}
	auditRetentionDays, err := intValue("AGENTX_AUDIT_RETENTION_DAYS", 0)
	if err != nil {
		return Config{}, err
	}
	cfg := Config{
		DeploymentMode: mode, DataDir: data, APIToken: os.Getenv("AGENTX_API_TOKEN"), Address: addr,
		DatabaseURL: os.Getenv("AGENTX_DATABASE_URL"), JWTSecret: os.Getenv("AGENTX_JWT_SECRET"),
		JWTIssuer: os.Getenv("AGENTX_JWT_ISSUER"), JWTAudience: os.Getenv("AGENTX_JWT_AUDIENCE"),
		OIDCIssuer: os.Getenv("AGENTX_OIDC_ISSUER"), OIDCBackchannelURL: os.Getenv("AGENTX_OIDC_BACKCHANNEL_URL"),
		OIDCCLIClientID: os.Getenv("AGENTX_OIDC_CLI_CLIENT_ID"), OIDCCLIScope: valueOrDefault("AGENTX_OIDC_CLI_SCOPE", "openid profile email offline_access"),
		ArtifactPublicKey: os.Getenv("AGENTX_ARTIFACT_PUBLIC_KEY"), ArtifactPublicKeys: csvValue("AGENTX_ARTIFACT_PUBLIC_KEYS"),
		WorkspaceID: os.Getenv("AGENTX_WORKSPACE_ID"), ArtifactStore: store,
		S3Endpoint: os.Getenv("AGENTX_S3_ENDPOINT"), S3AccessKey: os.Getenv("AGENTX_S3_ACCESS_KEY"),
		S3SecretKey: os.Getenv("AGENTX_S3_SECRET_KEY"), S3Bucket: valueOrDefault("AGENTX_S3_BUCKET", "agentx-artifacts"),
		S3Secure: secure, AllowedOrigins: csvValue("AGENTX_ALLOWED_ORIGINS"), TrustedProxies: csvValue("AGENTX_TRUSTED_PROXIES"),
		RateLimitPerMinute: rateLimit, AllowLegacyUnscoped: legacy, AllowHostedBootstrapAuth: bootstrapAuth,
		AllowInsecureHosted: insecureHosted,
		TermsURL:            os.Getenv("AGENTX_TERMS_URL"), PrivacyURL: os.Getenv("AGENTX_PRIVACY_URL"), SupportURL: os.Getenv("AGENTX_SUPPORT_URL"),
		AbuseEmail: os.Getenv("AGENTX_ABUSE_EMAIL"), SecurityEmail: os.Getenv("AGENTX_SECURITY_EMAIL"),
		ConsoleURL: os.Getenv("AGENTX_CONSOLE_URL"), SMTPAddress: os.Getenv("AGENTX_SMTP_ADDRESS"),
		SMTPUsername: os.Getenv("AGENTX_SMTP_USERNAME"), SMTPPassword: os.Getenv("AGENTX_SMTP_PASSWORD"), SMTPFrom: os.Getenv("AGENTX_SMTP_FROM"),
		SMTPTLSMode: strings.ToLower(valueOrDefault("AGENTX_SMTP_TLS_MODE", "starttls")), AllowUndeliveredInvites: allowUndeliveredInvites,
		ComplianceAdminIDs: csvValue("AGENTX_COMPLIANCE_ADMIN_IDS"),
		AuditRetentionDays: auditRetentionDays,
	}
	cfg.AuditRetentionInterval, err = durationValue("AGENTX_AUDIT_RETENTION_INTERVAL", 24*time.Hour)
	if err != nil {
		return Config{}, err
	}
	if len(cfg.AllowedOrigins) == 0 && mode == ModeLocal {
		cfg.AllowedOrigins = []string{"*"}
	}
	return cfg, cfg.Validate()
}

func (c Config) Validate() error {
	mode := c.DeploymentMode
	if mode == "" {
		mode = ModeLocal
	}
	if mode != ModeLocal && mode != ModeHosted {
		return fmt.Errorf("AGENTX_DEPLOYMENT_MODE must be %q or %q", ModeLocal, ModeHosted)
	}
	if c.ArtifactStore != "" && c.ArtifactStore != "file" && c.ArtifactStore != "s3" {
		return errors.New("AGENTX_ARTIFACT_STORE must be file or s3")
	}
	if c.RateLimitPerMinute < 0 {
		return errors.New("AGENTX_RATE_LIMIT_PER_MINUTE cannot be negative")
	}
	if c.AuditRetentionDays < 0 || c.AuditRetentionDays > 36500 {
		return errors.New("AGENTX_AUDIT_RETENTION_DAYS must be between 0 and 36500")
	}
	if c.AuditRetentionDays > 0 && c.AuditRetentionInterval < time.Minute {
		return errors.New("AGENTX_AUDIT_RETENTION_INTERVAL must be at least one minute")
	}
	complianceAdmins := make(map[string]struct{}, len(c.ComplianceAdminIDs))
	for _, id := range c.ComplianceAdminIDs {
		if len(id) > 2048 || strings.ContainsAny(id, "\r\n\x00") {
			return errors.New("AGENTX_COMPLIANCE_ADMIN_IDS contains an invalid principal ID")
		}
		if _, exists := complianceAdmins[id]; exists {
			return errors.New("AGENTX_COMPLIANCE_ADMIN_IDS contains a duplicate principal ID")
		}
		complianceAdmins[id] = struct{}{}
	}
	if (c.OIDCIssuer == "") != (c.JWTAudience == "") {
		return errors.New("AGENTX_OIDC_ISSUER and AGENTX_JWT_AUDIENCE must be configured together")
	}
	if c.OIDCBackchannelURL != "" {
		if c.OIDCIssuer == "" {
			return errors.New("AGENTX_OIDC_BACKCHANNEL_URL requires AGENTX_OIDC_ISSUER")
		}
		if err := validateHTTPURL("AGENTX_OIDC_BACKCHANNEL_URL", c.OIDCBackchannelURL); err != nil {
			return err
		}
	}
	if c.OIDCIssuer != "" {
		if err := validateHTTPURL("AGENTX_OIDC_ISSUER", c.OIDCIssuer); err != nil {
			return err
		}
	}
	if c.OIDCCLIClientID != "" && c.OIDCIssuer == "" {
		return errors.New("AGENTX_OIDC_CLI_CLIENT_ID requires AGENTX_OIDC_ISSUER")
	}
	if len(c.OIDCCLIClientID) > 256 || strings.ContainsAny(c.OIDCCLIClientID, "\r\n\x00") {
		return errors.New("AGENTX_OIDC_CLI_CLIENT_ID must be at most 256 characters and contain no control characters")
	}
	if c.OIDCCLIClientID != "" && c.OIDCCLIScope == "" {
		return errors.New("AGENTX_OIDC_CLI_SCOPE is required with AGENTX_OIDC_CLI_CLIENT_ID")
	}
	if len(c.OIDCCLIScope) > 1024 || strings.ContainsAny(c.OIDCCLIScope, "\r\n\x00") {
		return errors.New("AGENTX_OIDC_CLI_SCOPE must be non-empty, bounded, and contain no control characters")
	}
	if c.APIToken != "" && len(c.APIToken) < 32 {
		return errors.New("AGENTX_API_TOKEN must contain at least 32 characters")
	}
	if c.JWTSecret != "" && len(c.JWTSecret) < 32 {
		return errors.New("AGENTX_JWT_SECRET must contain at least 32 characters")
	}
	if err := validateOrigins(c.AllowedOrigins, mode == ModeHosted, mode == ModeHosted && !c.AllowInsecureHosted); err != nil {
		return err
	}
	for _, proxy := range c.TrustedProxies {
		if net.ParseIP(proxy) == nil {
			if _, _, err := net.ParseCIDR(proxy); err != nil {
				return fmt.Errorf("invalid AGENTX_TRUSTED_PROXIES entry %q", proxy)
			}
		}
	}
	if c.ArtifactStore == "s3" && (c.S3Endpoint == "" || c.S3AccessKey == "" || c.S3SecretKey == "" || c.S3Bucket == "") {
		return errors.New("S3 artifact storage requires endpoint, access key, secret key, and bucket")
	}
	for name, value := range map[string]string{"AGENTX_TERMS_URL": c.TermsURL, "AGENTX_PRIVACY_URL": c.PrivacyURL, "AGENTX_SUPPORT_URL": c.SupportURL} {
		if value != "" {
			if err := validateHTTPURL(name, value); err != nil {
				return err
			}
		}
	}
	for name, value := range map[string]string{"AGENTX_ABUSE_EMAIL": c.AbuseEmail, "AGENTX_SECURITY_EMAIL": c.SecurityEmail} {
		if value != "" {
			if err := validateEmail(name, value); err != nil {
				return err
			}
		}
	}
	if err := validateInvitationDelivery(c, mode); err != nil {
		return err
	}
	if mode == ModeHosted {
		if c.DatabaseURL == "" {
			return errors.New("hosted mode requires AGENTX_DATABASE_URL")
		}
		if c.OIDCIssuer == "" {
			return errors.New("hosted mode requires AGENTX_OIDC_ISSUER and AGENTX_JWT_AUDIENCE")
		}
		if c.OIDCCLIClientID == "" {
			return errors.New("hosted mode requires AGENTX_OIDC_CLI_CLIENT_ID for CLI device authorization")
		}
		if len(c.ComplianceAdminIDs) == 0 {
			return errors.New("hosted mode requires AGENTX_COMPLIANCE_ADMIN_IDS")
		}
		if c.AuditRetentionDays == 0 {
			return errors.New("hosted mode requires AGENTX_AUDIT_RETENTION_DAYS")
		}
		if c.ArtifactStore != "s3" {
			return errors.New("hosted mode requires AGENTX_ARTIFACT_STORE=s3")
		}
		if c.AllowLegacyUnscoped {
			return errors.New("hosted mode cannot enable legacy unscoped routes")
		}
		for name, value := range map[string]string{"AGENTX_TERMS_URL": c.TermsURL, "AGENTX_PRIVACY_URL": c.PrivacyURL, "AGENTX_SUPPORT_URL": c.SupportURL, "AGENTX_ABUSE_EMAIL": c.AbuseEmail, "AGENTX_SECURITY_EMAIL": c.SecurityEmail} {
			if strings.TrimSpace(value) == "" {
				return fmt.Errorf("hosted mode requires %s", name)
			}
		}
		if (c.APIToken != "" || c.JWTSecret != "") && !c.AllowHostedBootstrapAuth {
			return errors.New("hosted mode cannot enable API token or HMAC authentication unless AGENTX_ALLOW_HOSTED_BOOTSTRAP_AUTH=true")
		}
		if !c.AllowInsecureHosted {
			for name, value := range map[string]string{"AGENTX_TERMS_URL": c.TermsURL, "AGENTX_PRIVACY_URL": c.PrivacyURL, "AGENTX_SUPPORT_URL": c.SupportURL} {
				if err := requireHTTPS(name, value); err != nil {
					return err
				}
			}
			if err := requireHTTPS("AGENTX_OIDC_ISSUER", c.OIDCIssuer); err != nil {
				return err
			}
			if c.OIDCBackchannelURL != "" {
				if err := requireHTTPS("AGENTX_OIDC_BACKCHANNEL_URL", c.OIDCBackchannelURL); err != nil {
					return err
				}
			}
			if !c.S3Secure {
				return errors.New("hosted mode requires AGENTX_S3_SECURE=true")
			}
			if err := requirePostgreSQLTLS(c.DatabaseURL); err != nil {
				return err
			}
			if err := requireHTTPS("AGENTX_CONSOLE_URL", c.ConsoleURL); err != nil {
				return err
			}
		}
		principalPrefix := c.OIDCIssuer + "|"
		for _, id := range c.ComplianceAdminIDs {
			if !strings.HasPrefix(id, principalPrefix) || len(id) == len(principalPrefix) {
				return errors.New("AGENTX_COMPLIANCE_ADMIN_IDS entries must use the configured OIDC issuer|subject format")
			}
		}
	}
	return nil
}

func validateInvitationDelivery(c Config, mode string) error {
	if c.AllowUndeliveredInvites && (mode != ModeHosted || !c.AllowInsecureHosted) {
		return errors.New("AGENTX_ALLOW_UNDELIVERED_INVITATIONS is allowed only with isolated insecure hosted staging")
	}
	configured := c.SMTPAddress != "" || c.SMTPUsername != "" || c.SMTPPassword != "" || c.SMTPFrom != "" || c.ConsoleURL != ""
	if mode == ModeHosted && !c.AllowUndeliveredInvites {
		configured = true
	}
	if !configured {
		return nil
	}
	if c.SMTPAddress == "" || c.SMTPFrom == "" || c.ConsoleURL == "" {
		return errors.New("invitation delivery requires AGENTX_SMTP_ADDRESS, AGENTX_SMTP_FROM, and AGENTX_CONSOLE_URL")
	}
	host, portText, err := net.SplitHostPort(c.SMTPAddress)
	if err != nil || strings.TrimSpace(host) == "" {
		return errors.New("invalid AGENTX_SMTP_ADDRESS; expected host:port")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return errors.New("invalid AGENTX_SMTP_ADDRESS port")
	}
	if err := validateEmail("AGENTX_SMTP_FROM", c.SMTPFrom); err != nil {
		return err
	}
	if err := validateHTTPURL("AGENTX_CONSOLE_URL", c.ConsoleURL); err != nil {
		return err
	}
	if (c.SMTPUsername == "") != (c.SMTPPassword == "") {
		return errors.New("AGENTX_SMTP_USERNAME and AGENTX_SMTP_PASSWORD must be configured together")
	}
	if len(c.SMTPUsername) > 1024 || strings.ContainsAny(c.SMTPUsername, "\r\n\x00") || len(c.SMTPPassword) > 4096 || strings.ContainsAny(c.SMTPPassword, "\r\n\x00") {
		return errors.New("invalid SMTP credentials")
	}
	if c.SMTPTLSMode != "tls" && c.SMTPTLSMode != "starttls" && c.SMTPTLSMode != "plain" {
		return errors.New("AGENTX_SMTP_TLS_MODE must be tls, starttls, or plain")
	}
	if mode == ModeHosted && !c.AllowInsecureHosted {
		if c.SMTPTLSMode == "plain" {
			return errors.New("hosted mode requires encrypted SMTP transport")
		}
		if c.SMTPUsername == "" || c.SMTPPassword == "" {
			return errors.New("hosted mode requires AGENTX_SMTP_USERNAME and AGENTX_SMTP_PASSWORD")
		}
	}
	return nil
}

func validateEmail(name, value string) error {
	value = strings.TrimSpace(value)
	address, err := mail.ParseAddress(value)
	if err != nil || address.Name != "" || address.Address != value || len(value) > 254 || strings.ContainsAny(value, "\r\n\x00") {
		return fmt.Errorf("invalid %s value", name)
	}
	return nil
}

func validateOrigins(origins []string, hosted, requireHTTPS bool) error {
	if hosted && len(origins) == 0 {
		return errors.New("hosted mode requires AGENTX_ALLOWED_ORIGINS")
	}
	for _, origin := range origins {
		if origin == "*" {
			if hosted {
				return errors.New("hosted mode does not allow wildcard CORS origins")
			}
			continue
		}
		parsed, err := url.Parse(origin)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
			return fmt.Errorf("invalid AGENTX_ALLOWED_ORIGINS entry %q", origin)
		}
		if requireHTTPS && parsed.Scheme != "https" {
			return fmt.Errorf("hosted mode requires HTTPS AGENTX_ALLOWED_ORIGINS, got %q", origin)
		}
	}
	return nil
}

func requireHTTPS(name, value string) error {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" {
		return fmt.Errorf("hosted mode requires HTTPS %s", name)
	}
	return nil
}

func requirePostgreSQLTLS(value string) error {
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") {
		return errors.New("hosted mode requires a PostgreSQL AGENTX_DATABASE_URL")
	}
	switch parsed.Query().Get("sslmode") {
	case "require", "verify-ca", "verify-full":
		return nil
	default:
		return errors.New("hosted mode requires AGENTX_DATABASE_URL sslmode=require, verify-ca, or verify-full")
	}
}

func validateHTTPURL(name, value string) error {
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("invalid %s value %q", name, value)
	}
	return nil
}

func valueOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func csvValue(name string) []string {
	var values []string
	for _, value := range strings.Split(os.Getenv(name), ",") {
		if value = strings.TrimSpace(value); value != "" {
			values = append(values, value)
		}
	}
	return values
}

func boolValue(name string, fallback bool) (bool, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("%s must be a boolean: %w", name, err)
	}
	return value, nil
}

func intValue(name string, fallback int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", name, err)
	}
	return value, nil
}

func durationValue(name string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s must be a duration: %w", name, err)
	}
	return value, nil
}
