package config

import (
	"strings"
	"testing"
	"time"
)

func hostedConfig() Config {
	return Config{
		DeploymentMode:         ModeHosted,
		DatabaseURL:            "postgres://agentx:secret@postgres/agentx?sslmode=verify-full",
		OIDCIssuer:             "https://id.example.com",
		JWTAudience:            "agentx",
		OIDCCLIClientID:        "agentx-cli",
		OIDCCLIScope:           "openid profile email offline_access",
		ArtifactStore:          "s3",
		S3Endpoint:             "s3.example.com",
		S3AccessKey:            "access",
		S3SecretKey:            "secret",
		S3Bucket:               "artifacts",
		S3Secure:               true,
		AllowedOrigins:         []string{"https://console.example.com"},
		RateLimitPerMinute:     600,
		TermsURL:               "https://agentx.example.com/terms",
		PrivacyURL:             "https://agentx.example.com/privacy",
		SupportURL:             "https://agentx.example.com/support",
		AbuseEmail:             "abuse@agentx.example.com",
		SecurityEmail:          "security@agentx.example.com",
		ConsoleURL:             "https://console.example.com",
		SMTPAddress:            "smtp.example.com:465",
		SMTPUsername:           "mailer",
		SMTPPassword:           "mail-secret",
		SMTPFrom:               "notifications@agentx.example.com",
		SMTPTLSMode:            "tls",
		ComplianceAdminIDs:     []string{"https://id.example.com|compliance-admin"},
		AuditRetentionDays:     365,
		AuditRetentionInterval: 24 * time.Hour,
	}
}

func TestHostedConfigRequiresProductionDependencies(t *testing.T) {
	tests := []struct {
		name string
		edit func(*Config)
		want string
	}{
		{"database", func(c *Config) { c.DatabaseURL = "" }, "DATABASE_URL"},
		{"oidc", func(c *Config) { c.OIDCIssuer, c.JWTAudience = "", "" }, "OIDC_ISSUER"},
		{"cli oidc client", func(c *Config) { c.OIDCCLIClientID = "" }, "OIDC_CLI_CLIENT_ID"},
		{"s3", func(c *Config) { c.ArtifactStore = "file" }, "ARTIFACT_STORE=s3"},
		{"origins", func(c *Config) { c.AllowedOrigins = nil }, "ALLOWED_ORIGINS"},
		{"wildcard origin", func(c *Config) { c.AllowedOrigins = []string{"*"} }, "wildcard"},
		{"legacy routes", func(c *Config) { c.AllowLegacyUnscoped = true }, "legacy unscoped"},
		{"database tls", func(c *Config) { c.DatabaseURL = "postgres://agentx:secret@postgres/agentx?sslmode=disable" }, "sslmode"},
		{"s3 tls", func(c *Config) { c.S3Secure = false }, "S3_SECURE"},
		{"issuer tls", func(c *Config) { c.OIDCIssuer = "http://id.example.com" }, "HTTPS AGENTX_OIDC_ISSUER"},
		{"issuer credentials", func(c *Config) { c.OIDCIssuer = "https://user:pass@id.example.com" }, "invalid AGENTX_OIDC_ISSUER"},
		{"backchannel tls", func(c *Config) { c.OIDCBackchannelURL = "http://dex.internal/dex" }, "HTTPS AGENTX_OIDC_BACKCHANNEL_URL"},
		{"console tls", func(c *Config) { c.AllowedOrigins = []string{"http://console.example.com"} }, "HTTPS AGENTX_ALLOWED_ORIGINS"},
		{"console origin components", func(c *Config) { c.AllowedOrigins = []string{"https://user@console.example.com?tenant=one"} }, "invalid AGENTX_ALLOWED_ORIGINS"},
		{"terms", func(c *Config) { c.TermsURL = "" }, "AGENTX_TERMS_URL"},
		{"privacy", func(c *Config) { c.PrivacyURL = "" }, "AGENTX_PRIVACY_URL"},
		{"support", func(c *Config) { c.SupportURL = "" }, "AGENTX_SUPPORT_URL"},
		{"abuse", func(c *Config) { c.AbuseEmail = "" }, "AGENTX_ABUSE_EMAIL"},
		{"security", func(c *Config) { c.SecurityEmail = "" }, "AGENTX_SECURITY_EMAIL"},
		{"smtp address", func(c *Config) { c.SMTPAddress = "" }, "AGENTX_SMTP_ADDRESS"},
		{"smtp authentication", func(c *Config) { c.SMTPPassword = "" }, "configured together"},
		{"smtp transport", func(c *Config) { c.SMTPTLSMode = "plain" }, "encrypted SMTP"},
		{"console url", func(c *Config) { c.ConsoleURL = "" }, "AGENTX_CONSOLE_URL"},
		{"console tls", func(c *Config) { c.ConsoleURL = "http://console.example.com" }, "HTTPS AGENTX_CONSOLE_URL"},
		{"compliance admin", func(c *Config) { c.ComplianceAdminIDs = nil }, "AGENTX_COMPLIANCE_ADMIN_IDS"},
		{"compliance admin issuer", func(c *Config) { c.ComplianceAdminIDs = []string{"https://other.example.com|admin"} }, "issuer|subject"},
		{"compliance admin subject", func(c *Config) { c.ComplianceAdminIDs = []string{"https://id.example.com|"} }, "issuer|subject"},
		{"duplicate compliance admin", func(c *Config) {
			c.ComplianceAdminIDs = []string{"https://id.example.com|admin", "https://id.example.com|admin"}
		}, "duplicate"},
		{"audit retention", func(c *Config) { c.AuditRetentionDays = 0 }, "AGENTX_AUDIT_RETENTION_DAYS"},
		{"legal credentials", func(c *Config) { c.TermsURL = "https://user:pass@agentx.example.com/terms" }, "invalid AGENTX_TERMS_URL"},
		{"legal tls", func(c *Config) { c.PrivacyURL = "http://agentx.example.com/privacy" }, "HTTPS AGENTX_PRIVACY_URL"},
		{"contact injection", func(c *Config) { c.AbuseEmail = "abuse@example.com\nBcc: victim@example.com" }, "invalid AGENTX_ABUSE_EMAIL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := hostedConfig()
			tt.edit(&cfg)
			if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected %q validation error, got %v", tt.want, err)
			}
		})
	}
}

func TestHostedConfigAcceptsCompleteConfiguration(t *testing.T) {
	if err := hostedConfig().Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestHostedConfigRejectsBootstrapAuthenticationByDefault(t *testing.T) {
	for _, configure := range []func(*Config){
		func(cfg *Config) { cfg.APIToken = strings.Repeat("a", 32) },
		func(cfg *Config) { cfg.JWTSecret = strings.Repeat("b", 32) },
	} {
		cfg := hostedConfig()
		configure(&cfg)
		if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "ALLOW_HOSTED_BOOTSTRAP_AUTH") {
			t.Fatalf("expected hosted bootstrap authentication rejection, got %v", err)
		}
	}
}

func TestHostedConfigAllowsExplicitStagingBootstrapAuthentication(t *testing.T) {
	cfg := hostedConfig()
	cfg.APIToken = strings.Repeat("a", 32)
	cfg.AllowHostedBootstrapAuth = true
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestHostedConfigAllowsExplicitInsecureStagingTransports(t *testing.T) {
	cfg := hostedConfig()
	cfg.DatabaseURL = "postgres://agentx:secret@postgres/agentx?sslmode=disable"
	cfg.OIDCIssuer = "http://localhost:5556/dex"
	cfg.ComplianceAdminIDs = []string{"http://localhost:5556/dex|compliance-admin"}
	cfg.S3Secure = false
	cfg.AllowedOrigins = []string{"http://localhost:5178"}
	cfg.AllowInsecureHosted = true
	cfg.TermsURL = "http://localhost:5178/terms-placeholder"
	cfg.PrivacyURL = "http://localhost:5178/privacy-placeholder"
	cfg.SupportURL = "http://localhost:5178/support-placeholder"
	cfg.ConsoleURL = "http://localhost:5178"
	cfg.SMTPAddress = "mailpit:1025"
	cfg.SMTPUsername = ""
	cfg.SMTPPassword = ""
	cfg.SMTPFrom = "notifications-staging@example.invalid"
	cfg.SMTPTLSMode = "plain"
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestHostedConfigAllowsUndeliveredInvitationsOnlyInIsolatedStaging(t *testing.T) {
	cfg := hostedConfig()
	cfg.AllowUndeliveredInvites = true
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "isolated insecure hosted staging") {
		t.Fatalf("expected production invitation delivery bypass rejection, got %v", err)
	}
	cfg.AllowInsecureHosted = true
	cfg.SMTPAddress, cfg.SMTPUsername, cfg.SMTPPassword, cfg.SMTPFrom, cfg.ConsoleURL = "", "", "", "", ""
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected isolated staging invitation bypass, got %v", err)
	}
}

func TestWeakBootstrapSecretsAreRejected(t *testing.T) {
	cfg := hostedConfig()
	cfg.APIToken = "short"
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "32 characters") {
		t.Fatalf("expected weak token rejection, got %v", err)
	}
}

func TestLoadArtifactPublicKeyRotationRing(t *testing.T) {
	t.Setenv("AGENTX_ARTIFACT_PUBLIC_KEYS", "new-key, old-key")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.ArtifactPublicKeys) != 2 || cfg.ArtifactPublicKeys[0] != "new-key" || cfg.ArtifactPublicKeys[1] != "old-key" {
		t.Fatalf("unexpected key ring: %#v", cfg.ArtifactPublicKeys)
	}
}

func TestLoadAuditRetentionConfiguration(t *testing.T) {
	t.Setenv("AGENTX_AUDIT_RETENTION_DAYS", "90")
	t.Setenv("AGENTX_AUDIT_RETENTION_INTERVAL", "6h")
	t.Setenv("AGENTX_COMPLIANCE_ADMIN_IDS", "issuer|admin-one, issuer|admin-two")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AuditRetentionDays != 90 || cfg.AuditRetentionInterval != 6*time.Hour || len(cfg.ComplianceAdminIDs) != 2 {
		t.Fatalf("unexpected lifecycle configuration: %+v", cfg)
	}

	t.Setenv("AGENTX_AUDIT_RETENTION_DAYS", "ninety")
	if _, err = Load(); err == nil || !strings.Contains(err.Error(), "must be an integer") {
		t.Fatalf("expected invalid retention duration to fail, got %v", err)
	}
}
