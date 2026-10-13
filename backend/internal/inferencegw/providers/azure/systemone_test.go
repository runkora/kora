package azure

import (
	"context"
	"errors"
	"testing"

	"github.com/clear-route/orbit/internal/inferencegw"
)

func TestSystemOneIsRefused(t *testing.T) {
	c := &Connector{name: "azure:eastus"}
	res, err := c.SystemOne(context.Background(), inferencegw.SystemOneRequest{Body: []byte(`{"model":"m"}`), Model: "m"})
	var ce *inferencegw.ConnectorError
	if res != nil || !errors.As(err, &ce) {
		t.Fatalf("res = %v, err = %v", res, err)
	}
	want := "provider azure:eastus does not serve the systemone API"
	if ce.Code != "MODEL_NOT_SUPPORTED" || !ce.Retryable || ce.ProviderMessage != want || ce.HTTPStatus != 400 || ce.Provider != "azure:eastus" {
		t.Errorf("error = %+v, want %q", ce, want)
	}
}
