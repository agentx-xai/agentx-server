package deploycheck

import (
	"errors"
	"fmt"
	"io"
	"net/mail"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

var (
	digestImagePattern   = regexp.MustCompile(`^[^@[:space:]]+@sha256:[a-f0-9]{64}$`)
	exampleDomainPattern = regexp.MustCompile(`(?i)\b(?:[a-z0-9-]+\.)*example\.(?:com|invalid)\b`)
	loopbackPattern      = regexp.MustCompile(`(?i)(?:^|[/@])localhost(?::|/|$)|\b127\.0\.0\.1\b|\[::1\]`)
)

var sensitiveConfigKeys = map[string]struct{}{
	"AGENTX_API_TOKEN":     {},
	"AGENTX_DATABASE_URL":  {},
	"AGENTX_JWT_SECRET":    {},
	"AGENTX_S3_ACCESS_KEY": {},
	"AGENTX_S3_SECRET_KEY": {},
	"AGENTX_SMTP_PASSWORD": {},
	"AGENTX_SMTP_USERNAME": {},
}

var requiredExternalSecretKeys = []string{
	"AGENTX_ARTIFACT_PUBLIC_KEYS",
	"AGENTX_DATABASE_URL",
	"AGENTX_S3_ACCESS_KEY",
	"AGENTX_S3_SECRET_KEY",
	"AGENTX_SMTP_PASSWORD",
	"AGENTX_SMTP_USERNAME",
}

type manifest struct {
	Kind       string            `yaml:"kind"`
	Metadata   metadata          `yaml:"metadata"`
	Data       map[string]string `yaml:"data"`
	StringData map[string]string `yaml:"stringData"`
	Spec       manifestSpec      `yaml:"spec"`
}

type metadata struct {
	Name string `yaml:"name"`
}

type manifestSpec struct {
	Template    podTemplate      `yaml:"template"`
	JobTemplate jobTemplate      `yaml:"jobTemplate"`
	Target      externalTarget   `yaml:"target"`
	Data        []externalSecret `yaml:"data"`
}

type jobTemplate struct {
	Spec struct {
		Template podTemplate `yaml:"template"`
	} `yaml:"spec"`
}

type podTemplate struct {
	Spec podSpec `yaml:"spec"`
}

type podSpec struct {
	InitContainers      []container `yaml:"initContainers"`
	Containers          []container `yaml:"containers"`
	EphemeralContainers []container `yaml:"ephemeralContainers"`
}

type container struct {
	Name    string          `yaml:"name"`
	Image   string          `yaml:"image"`
	Env     []envVar        `yaml:"env"`
	EnvFrom []envFromSource `yaml:"envFrom"`
}

type envVar struct {
	Name  string `yaml:"name"`
	Value string `yaml:"value"`
}

type externalTarget struct {
	Name string `yaml:"name"`
}

type externalSecret struct {
	SecretKey string `yaml:"secretKey"`
	RemoteRef struct {
		Key string `yaml:"key"`
	} `yaml:"remoteRef"`
}

type envFromSource struct {
	ConfigMapRef objectReference `yaml:"configMapRef"`
	SecretRef    objectReference `yaml:"secretRef"`
}

type objectReference struct {
	Name string `yaml:"name"`
}

// Check validates a fully rendered Kubernetes manifest before a production apply.
func Check(reader io.Reader) error {
	decoder := yaml.NewDecoder(reader)
	errorsByMessage := make(map[string]struct{})
	addError := func(format string, args ...any) {
		errorsByMessage[fmt.Sprintf(format, args...)] = struct{}{}
	}

	resourceCount := 0
	imageCount := 0
	hostedConfigCount := 0
	hostedConfigNames := make(map[string]struct{})
	configReferences := make(map[string]struct{})
	secretReferences := make(map[string]struct{})
	externalKeysByTarget := make(map[string]map[string]struct{})

	for document := 1; ; document++ {
		var node yaml.Node
		err := decoder.Decode(&node)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			addError("document %d is invalid YAML: %v", document, err)
			break
		}
		if len(node.Content) == 0 {
			continue
		}

		var item manifest
		if err := node.Decode(&item); err != nil {
			addError("document %d cannot be decoded: %v", document, err)
			continue
		}
		resourceCount++
		label := resourceLabel(item, document)
		scanScalars(node.Content[0], label, addError)

		if item.Kind == "Secret" && (len(item.Data) > 0 || len(item.StringData) > 0) {
			addError("%s contains secret material; use an external secret provider", label)
		}

		if item.Kind == "ConfigMap" {
			for key := range item.Data {
				if _, sensitive := sensitiveConfigKeys[key]; sensitive {
					addError("%s stores sensitive key %s in a ConfigMap", label, key)
				}
			}
			if _, hosted := item.Data["AGENTX_DEPLOYMENT_MODE"]; hosted {
				hostedConfigCount++
				hostedConfigNames[item.Metadata.Name] = struct{}{}
				validateHostedConfig(label, item.Data, addError)
			}
		}

		if item.Kind == "ExternalSecret" {
			target := item.Spec.Target.Name
			if target == "" {
				addError("%s must declare spec.target.name", label)
			}
			if _, exists := externalKeysByTarget[target]; !exists {
				externalKeysByTarget[target] = make(map[string]struct{})
			}
			for _, entry := range item.Spec.Data {
				if entry.SecretKey != "" {
					externalKeysByTarget[target][entry.SecretKey] = struct{}{}
					if strings.TrimSpace(entry.RemoteRef.Key) == "" {
						addError("%s mapping for %s has an empty remoteRef.key", label, entry.SecretKey)
					}
				}
			}
		}

		for _, spec := range podSpecs(item) {
			containers := append([]container{}, spec.InitContainers...)
			containers = append(containers, spec.Containers...)
			containers = append(containers, spec.EphemeralContainers...)
			for _, container := range containers {
				imageCount++
				if !digestImagePattern.MatchString(container.Image) {
					addError("%s container %q must use an immutable image@sha256 digest, got %q", label, container.Name, container.Image)
				}
				for _, variable := range container.Env {
					if _, sensitive := sensitiveConfigKeys[variable.Name]; sensitive && variable.Value != "" {
						addError("%s container %q embeds sensitive environment variable %s", label, container.Name, variable.Name)
					}
					validateEscapeHatch(label+" container "+strconv.Quote(container.Name), variable.Name, variable.Value, addError)
				}
				for _, source := range container.EnvFrom {
					if source.ConfigMapRef.Name != "" {
						configReferences[source.ConfigMapRef.Name] = struct{}{}
					}
					if source.SecretRef.Name != "" {
						secretReferences[source.SecretRef.Name] = struct{}{}
					}
				}
			}
		}
	}

	if resourceCount == 0 {
		addError("manifest contains no Kubernetes resources")
	}
	if imageCount == 0 {
		addError("manifest contains no workload container images")
	}
	if hostedConfigCount != 1 {
		addError("manifest must contain exactly one ConfigMap with AGENTX_DEPLOYMENT_MODE, found %d", hostedConfigCount)
	}
	for configName := range hostedConfigNames {
		if _, referenced := configReferences[configName]; !referenced {
			addError("hosted ConfigMap %q is not referenced by a workload", configName)
		}
	}
	completeExternalTarget := false
	for target, keys := range externalKeysByTarget {
		complete := target != ""
		for _, key := range requiredExternalSecretKeys {
			if _, present := keys[key]; !present {
				complete = false
			}
		}
		if complete {
			completeExternalTarget = true
			if _, referenced := secretReferences[target]; !referenced {
				addError("ExternalSecret target %q is not referenced by a workload", target)
			}
		}
	}
	if !completeExternalTarget {
		addError("manifest must map all required production keys into one ExternalSecret target")
	}

	if len(errorsByMessage) == 0 {
		return nil
	}
	messages := make([]string, 0, len(errorsByMessage))
	for message := range errorsByMessage {
		messages = append(messages, message)
	}
	sort.Strings(messages)
	return fmt.Errorf("production manifest preflight failed:\n- %s", strings.Join(messages, "\n- "))
}

func resourceLabel(item manifest, document int) string {
	if item.Kind == "" || item.Metadata.Name == "" {
		return fmt.Sprintf("document %d", document)
	}
	return item.Kind + "/" + item.Metadata.Name
}

func scanScalars(node *yaml.Node, label string, addError func(string, ...any)) {
	if node.Kind == yaml.ScalarNode && node.Tag == "!!str" {
		value := node.Value
		if strings.Contains(value, "REPLACE_WITH_") {
			addError("%s contains an unreplaced REPLACE_WITH_* sentinel", label)
		}
		if exampleDomainPattern.MatchString(value) {
			addError("%s contains an example domain in %q", label, value)
		}
		if loopbackPattern.MatchString(value) {
			addError("%s contains a loopback endpoint in %q", label, value)
		}
	}
	for _, child := range node.Content {
		scanScalars(child, label, addError)
	}
}

func podSpecs(item manifest) []podSpec {
	var specs []podSpec
	if len(item.Spec.Template.Spec.Containers)+len(item.Spec.Template.Spec.InitContainers)+len(item.Spec.Template.Spec.EphemeralContainers) > 0 {
		specs = append(specs, item.Spec.Template.Spec)
	}
	cronSpec := item.Spec.JobTemplate.Spec.Template.Spec
	if len(cronSpec.Containers)+len(cronSpec.InitContainers)+len(cronSpec.EphemeralContainers) > 0 {
		specs = append(specs, cronSpec)
	}
	return specs
}

func validateHostedConfig(label string, data map[string]string, addError func(string, ...any)) {
	requireEqual := map[string]string{
		"AGENTX_ARTIFACT_STORE":  "s3",
		"AGENTX_DEPLOYMENT_MODE": "hosted",
		"AGENTX_S3_SECURE":       "true",
	}
	for key, expected := range requireEqual {
		if data[key] != expected {
			addError("%s requires %s=%s", label, key, expected)
		}
	}

	required := []string{
		"AGENTX_ABUSE_EMAIL",
		"AGENTX_ALLOWED_ORIGINS",
		"AGENTX_AUDIT_RETENTION_DAYS",
		"AGENTX_COMPLIANCE_ADMIN_IDS",
		"AGENTX_CONSOLE_URL",
		"AGENTX_JWT_AUDIENCE",
		"AGENTX_OIDC_CLI_CLIENT_ID",
		"AGENTX_OIDC_CLI_SCOPE",
		"AGENTX_OIDC_ISSUER",
		"AGENTX_PRIVACY_URL",
		"AGENTX_S3_BUCKET",
		"AGENTX_S3_ENDPOINT",
		"AGENTX_SECURITY_EMAIL",
		"AGENTX_SMTP_ADDRESS",
		"AGENTX_SMTP_FROM",
		"AGENTX_SMTP_TLS_MODE",
		"AGENTX_SUPPORT_URL",
		"AGENTX_TERMS_URL",
	}
	for _, key := range required {
		if strings.TrimSpace(data[key]) == "" {
			addError("%s is missing %s", label, key)
		}
	}

	for _, key := range []string{"AGENTX_CONSOLE_URL", "AGENTX_OIDC_ISSUER", "AGENTX_PRIVACY_URL", "AGENTX_SUPPORT_URL", "AGENTX_TERMS_URL"} {
		if value := strings.TrimSpace(data[key]); value != "" && !isHTTPSURL(value) {
			addError("%s requires an HTTPS URL in %s", label, key)
		}
	}
	origins := splitCSV(data["AGENTX_ALLOWED_ORIGINS"])
	if len(origins) == 0 {
		addError("%s requires at least one AGENTX_ALLOWED_ORIGINS entry", label)
	}
	for _, origin := range origins {
		if origin == "*" || !isHTTPSURL(origin) {
			addError("%s requires explicit HTTPS AGENTX_ALLOWED_ORIGINS entries", label)
		}
	}
	for _, key := range []string{"AGENTX_ABUSE_EMAIL", "AGENTX_SECURITY_EMAIL"} {
		if value := strings.TrimSpace(data[key]); value != "" {
			address, err := mail.ParseAddress(value)
			if err != nil || address.Name != "" || address.Address != value {
				addError("%s contains invalid %s", label, key)
			}
		}
	}
	retention, err := strconv.Atoi(strings.TrimSpace(data["AGENTX_AUDIT_RETENTION_DAYS"]))
	if err != nil || retention <= 0 || retention > 36500 {
		addError("%s requires AGENTX_AUDIT_RETENTION_DAYS between 1 and 36500", label)
	}
	if tlsMode := strings.TrimSpace(data["AGENTX_SMTP_TLS_MODE"]); tlsMode != "tls" && tlsMode != "starttls" {
		addError("%s requires AGENTX_SMTP_TLS_MODE=tls or starttls", label)
	}

	issuer := strings.TrimSpace(data["AGENTX_OIDC_ISSUER"])
	seenPrincipals := make(map[string]struct{})
	principals := splitCSV(data["AGENTX_COMPLIANCE_ADMIN_IDS"])
	if len(principals) == 0 {
		addError("%s requires at least one AGENTX_COMPLIANCE_ADMIN_IDS entry", label)
	}
	for _, principal := range principals {
		if !strings.HasPrefix(principal, issuer+"|") || principal == issuer+"|" {
			addError("%s contains a compliance principal outside AGENTX_OIDC_ISSUER", label)
		}
		if _, exists := seenPrincipals[principal]; exists {
			addError("%s contains a duplicate compliance principal", label)
		}
		seenPrincipals[principal] = struct{}{}
	}

	for _, key := range []string{"AGENTX_ALLOW_HOSTED_BOOTSTRAP_AUTH", "AGENTX_ALLOW_INSECURE_HOSTED", "AGENTX_ALLOW_LEGACY_UNSCOPED", "AGENTX_ALLOW_UNDELIVERED_INVITATIONS"} {
		validateEscapeHatch(label, key, data[key], addError)
	}
}

func validateEscapeHatch(label, key, value string, addError func(string, ...any)) {
	if value == "" {
		return
	}
	enabled, err := strconv.ParseBool(strings.TrimSpace(value))
	if err != nil || enabled {
		addError("%s must not enable %s", label, key)
	}
}

func isHTTPSURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil && parsed.RawQuery == "" && parsed.Fragment == ""
}

func splitCSV(value string) []string {
	var values []string
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			values = append(values, item)
		}
	}
	return values
}
