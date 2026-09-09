package entity

type IdempotencyRecord struct {
	Fingerprint string  `json:"fingerprint"`
	Release     Release `json:"release"`
}

type DeviceIdempotencyRecord struct {
	Fingerprint string `json:"fingerprint"`
	Device      Device `json:"device"`
}
