package deploycheck

import (
	"strings"
	"testing"
)

const validConfig = `apiVersion: v1
kind: ConfigMap
metadata:
  name: agentx-api-config
data:
  AGENTX_DEPLOYMENT_MODE: hosted
  AGENTX_ARTIFACT_STORE: s3
  AGENTX_S3_ENDPOINT: objects.acme.test
  AGENTX_S3_BUCKET: agentx-production
  AGENTX_S3_SECURE: "true"
  AGENTX_OIDC_ISSUER: https://id.acme.test
  AGENTX_JWT_AUDIENCE: agentx
  AGENTX_OIDC_CLI_CLIENT_ID: agentx-cli
  AGENTX_OIDC_CLI_SCOPE: openid profile email offline_access
  AGENTX_ALLOWED_ORIGINS: https://console.acme.test
  AGENTX_CONSOLE_URL: https://console.acme.test
  AGENTX_SMTP_ADDRESS: smtp.acme.test:465
  AGENTX_SMTP_FROM: notifications@acme.test
  AGENTX_SMTP_TLS_MODE: tls
  AGENTX_ALLOW_LEGACY_UNSCOPED: "false"
  AGENTX_TERMS_URL: https://www.acme.test/terms
  AGENTX_PRIVACY_URL: https://www.acme.test/privacy
  AGENTX_SUPPORT_URL: https://www.acme.test/support
  AGENTX_ABUSE_EMAIL: abuse@acme.test
  AGENTX_SECURITY_EMAIL: security@acme.test
  AGENTX_COMPLIANCE_ADMIN_IDS: https://id.acme.test|compliance-admin
  AGENTX_AUDIT_RETENTION_DAYS: "365"
`

const validExternalSecret = `---
apiVersion: external-secrets.io/v1beta1
kind: ExternalSecret
metadata:
  name: agentx-api
spec:
  target:
    name: agentx-api-secrets
  data:
    - secretKey: AGENTX_DATABASE_URL
      remoteRef: { key: agentx/production/database-url }
    - secretKey: AGENTX_S3_ACCESS_KEY
      remoteRef: { key: agentx/production/s3-access-key }
    - secretKey: AGENTX_S3_SECRET_KEY
      remoteRef: { key: agentx/production/s3-secret-key }
    - secretKey: AGENTX_ARTIFACT_PUBLIC_KEYS
      remoteRef: { key: agentx/production/artifact-public-keys }
    - secretKey: AGENTX_SMTP_USERNAME
      remoteRef: { key: agentx/production/smtp-username }
    - secretKey: AGENTX_SMTP_PASSWORD
      remoteRef: { key: agentx/production/smtp-password }
`

const validDeployment = `---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: agentx-api
spec:
  template:
    spec:
      containers:
        - name: api
          image: registry.acme.test/agentx-api@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
          envFrom:
            - configMapRef: { name: agentx-api-config }
            - secretRef: { name: agentx-api-secrets }
`

func validManifest() string {
	return validConfig + validExternalSecret + validDeployment
}

func TestCheckAcceptsProductionManifest(t *testing.T) {
	if err := Check(strings.NewReader(validManifest())); err != nil {
		t.Fatalf("valid production manifest rejected: %v", err)
	}
}

func TestCheckRejectsUnsafeProductionManifest(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		message string
	}{
		{name: "empty", input: "", message: "no Kubernetes resources"},
		{name: "invalid yaml", input: "kind: [", message: "invalid YAML"},
		{name: "placeholder", input: strings.Replace(validManifest(), "https://www.acme.test/terms", "REPLACE_WITH_HTTPS_TERMS_URL", 1), message: "unreplaced REPLACE_WITH_*"},
		{name: "example domain", input: strings.Replace(validManifest(), "console.acme.test", "console.agentx.example.com", 1), message: "example domain"},
		{name: "mutable image", input: strings.Replace(validManifest(), "registry.acme.test/agentx-api@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "registry.acme.test/agentx-api:latest", 1), message: "immutable image@sha256"},
		{name: "insecure override", input: strings.Replace(validManifest(), "  AGENTX_ALLOW_LEGACY_UNSCOPED: \"false\"", "  AGENTX_ALLOW_LEGACY_UNSCOPED: \"false\"\n  AGENTX_ALLOW_INSECURE_HOSTED: \"true\"", 1), message: "must not enable AGENTX_ALLOW_INSECURE_HOSTED"},
		{name: "plaintext database url", input: strings.Replace(validManifest(), "  AGENTX_ARTIFACT_STORE: s3", "  AGENTX_ARTIFACT_STORE: s3\n  AGENTX_DATABASE_URL: postgres://database.acme.test/agentx", 1), message: "stores sensitive key AGENTX_DATABASE_URL"},
		{name: "missing external secrets", input: validConfig + validDeployment, message: "must map all required production keys"},
		{name: "unwired hosted config", input: strings.Replace(validManifest(), "            - configMapRef: { name: agentx-api-config }\n", "", 1), message: "hosted ConfigMap \"agentx-api-config\" is not referenced"},
		{name: "unwired external secret", input: strings.Replace(validManifest(), "            - secretRef: { name: agentx-api-secrets }\n", "", 1), message: "ExternalSecret target \"agentx-api-secrets\" is not referenced"},
		{name: "empty remote secret reference", input: strings.Replace(validManifest(), "agentx/production/database-url", "", 1), message: "empty remoteRef.key"},
		{name: "wrong compliance issuer", input: strings.Replace(validManifest(), "https://id.acme.test|compliance-admin", "https://other.acme.test|compliance-admin", 1), message: "compliance principal outside"},
		{name: "plaintext secret resource", input: validManifest() + "---\napiVersion: v1\nkind: Secret\nmetadata: { name: unsafe }\nstringData: { password: exposed }\n", message: "contains secret material"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := Check(strings.NewReader(test.input))
			if err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("expected error containing %q, got %v", test.message, err)
			}
		})
	}
}
