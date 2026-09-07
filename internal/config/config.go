package config

import "os"

type Config struct {
	DataDir             string
	APIToken            string
	Address             string
	DatabaseURL         string
	JWTSecret           string
	JWTIssuer           string
	JWTAudience         string
	OIDCIssuer          string
	ArtifactPublicKey   string
	WorkspaceID         string
	ArtifactStore       string
	S3Endpoint          string
	S3AccessKey         string
	S3SecretKey         string
	S3Bucket            string
	S3Secure            bool
	AllowLegacyUnscoped bool
}

func Load() Config {
	data := os.Getenv("AGENTX_DATA_DIR")
	if data == "" {
		data = "./data"
	}
	addr := os.Getenv("AGENTX_HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	store := os.Getenv("AGENTX_ARTIFACT_STORE")
	if store == "" {
		store = "file"
	}
	secure := os.Getenv("AGENTX_S3_SECURE") == "1" || os.Getenv("AGENTX_S3_SECURE") == "true"
	legacy := os.Getenv("AGENTX_ALLOW_LEGACY_UNSCOPED") == "1" || os.Getenv("AGENTX_ALLOW_LEGACY_UNSCOPED") == "true"
	return Config{DataDir: data, APIToken: os.Getenv("AGENTX_API_TOKEN"), Address: addr, DatabaseURL: os.Getenv("AGENTX_DATABASE_URL"), JWTSecret: os.Getenv("AGENTX_JWT_SECRET"), JWTIssuer: os.Getenv("AGENTX_JWT_ISSUER"), JWTAudience: os.Getenv("AGENTX_JWT_AUDIENCE"), OIDCIssuer: os.Getenv("AGENTX_OIDC_ISSUER"), ArtifactPublicKey: os.Getenv("AGENTX_ARTIFACT_PUBLIC_KEY"), WorkspaceID: os.Getenv("AGENTX_WORKSPACE_ID"), ArtifactStore: store, S3Endpoint: os.Getenv("AGENTX_S3_ENDPOINT"), S3AccessKey: os.Getenv("AGENTX_S3_ACCESS_KEY"), S3SecretKey: os.Getenv("AGENTX_S3_SECRET_KEY"), S3Bucket: os.Getenv("AGENTX_S3_BUCKET"), S3Secure: secure, AllowLegacyUnscoped: legacy}
}
