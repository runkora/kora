package agents

import (
	"context"
	"net/http"
	"sort"

	"github.com/clear-route/orbit/internal/platform/api"
)

// SandboxImageView is a registered sandbox image as an environment author sees it.
type SandboxImageView struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Image       string `json:"image"`
	Digest      string `json:"digest,omitempty"`
	Valid       bool   `json:"valid"`
	// Problem says why an invalid image cannot be selected.
	Problem string `json:"problem,omitempty"`
	Warm    bool   `json:"warm"`
}

// ListSandboxImages lists the caller's tenant's registered sandbox images by name.
func (s *EnvironmentService) ListSandboxImages(ctx context.Context) ([]SandboxImageView, error) {
	out := []SandboxImageView{}
	if s.images == nil {
		return out, nil
	}
	imgs, err := s.images.SandboxImages(ctx, s.cfg.TenantNamespaceFn(tenantFromContext(ctx)))
	if err != nil {
		return nil, err
	}
	for i := range imgs {
		img := &imgs[i]
		v := SandboxImageView{
			Name:        img.Name,
			Description: img.Spec.Description,
			Image:       img.Spec.Image,
			Digest:      img.Status.Digest,
			Valid:       img.Valid(),
			Warm:        img.Spec.Warm,
		}
		if !v.Valid {
			v.Problem = sandboxImageProblem(img)
		}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (c *Controller) handleListSandboxImages(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	imgs, err := c.environments.ListSandboxImages(ctx)
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger,
			api.InternalError(api.ErrCodeInternalError, "failed to list sandbox images", nil), err)
		return
	}
	api.JSON(ctx, w, c.logger, http.StatusOK, map[string]any{"sandbox_images": imgs})
}
