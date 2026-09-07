package dockerx

import (
	"context"
	"sort"
	"strings"
)

// DistributionImage is registry metadata resolved without pulling an image or
// changing local Docker state. RegistryAuth is Docker's base64url-encoded auth
// JSON and is supplied only to the Engine request; it is never part of the
// returned evidence.
type DistributionImage struct {
	Reference string   `json:"reference"`
	Digest    string   `json:"digest"`
	MediaType string   `json:"mediaType,omitempty"`
	Platforms []string `json:"platforms"`
}

func (c *Client) ResolveDistributionImage(
	ctx context.Context,
	reference, registryAuth string,
) (*DistributionImage, error) {
	cli, err := c.api()
	if err != nil {
		return nil, err
	}
	inspection, err := cli.DistributionInspect(ctx, reference, registryAuth)
	if err != nil {
		return nil, err
	}
	result := &DistributionImage{
		Reference: reference, Digest: inspection.Descriptor.Digest.String(),
		MediaType: inspection.Descriptor.MediaType, Platforms: []string{},
	}
	for _, platform := range inspection.Platforms {
		value := strings.Trim(platform.OS+"/"+platform.Architecture, "/")
		if platform.Variant != "" {
			value += "/" + platform.Variant
		}
		if value != "" {
			result.Platforms = append(result.Platforms, value)
		}
	}
	sort.Strings(result.Platforms)
	return result, nil
}
