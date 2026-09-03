package aliyundrive_share2_open

import (
	"context"
	"errors"
	"testing"

	"github.com/OpenListTeam/OpenList/v4/internal/byoa"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
)

func TestAliyunBYOARequiresBrowserCredential(t *testing.T) {
	d := &AliyundriveShare2Open{}

	_, err := d.Link(context.Background(), nil, model.LinkArgs{})
	if err == nil {
		t.Fatal("Link() expected BYOA auth error")
	}
	if !byoa.IsAuthRequired(err) {
		t.Fatalf("Link() error = %T %v, want AuthRequiredError", err, err)
	}

	var authErr *byoa.AuthRequiredError
	if !errors.As(err, &authErr) {
		t.Fatalf("errors.As() failed for %T", err)
	}
	if authErr.Provider != byoa.ProviderAliyun {
		t.Fatalf("provider = %q, want %q", authErr.Provider, byoa.ProviderAliyun)
	}
}
