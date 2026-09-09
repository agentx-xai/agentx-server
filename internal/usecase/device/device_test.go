package device

import (
	"agentx/server/internal/entity"
	"agentx/server/internal/repo/file"
	"context"
	"errors"
	"strings"
	"testing"
)

type failingAudit struct{ err error }

func (a failingAudit) List(context.Context) ([]entity.AuditEvent, error) { return nil, nil }
func (a failingAudit) Append(context.Context, entity.AuditEvent) error   { return a.err }

func TestRegisterReportsAuditFailure(t *testing.T) {
	devices := file.NewDeviceRepo(t.TempDir())
	service := New(devices, failingAudit{err: errors.New("disk full")})
	created, err := service.Register(context.Background(), entity.Device{Name: "laptop"})
	if err == nil || !strings.Contains(err.Error(), "device committed but audit append failed") {
		t.Fatalf("expected explicit audit failure, got %v", err)
	}
	items, listErr := devices.List(context.Background())
	if listErr != nil || len(items) != 1 || items[0].ID != created.ID {
		t.Fatalf("committed device missing after reported audit failure: %+v, %v", items, listErr)
	}
}
