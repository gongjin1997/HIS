package oci

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"
	orasoci "oras.land/oras-go/v2/content/oci"
)

const (
	// ArtifactTypeDeclarativePlugin is the OCI 1.1 artifactType for kinstall declarative plugins (T-M5-21).
	ArtifactTypeDeclarativePlugin = "application/vnd.kinstall.plugin.v1+yaml"
	// ArtifactTypeBundle is the OCI 1.1 artifactType for kinstall offline media bundles (T-M1-16).
	ArtifactTypeBundle = "application/vnd.kinstall.bundle.v1"
)

// PackAndPushArtifact writes files into a deterministic local OCI Image Layout via oras-go/v2 (T-M1-16, T-M5-21).
func PackAndPushArtifact(ctx context.Context, layoutDir, tag, artifactType string, files map[string][]byte) (ocispec.Descriptor, error) {
	store, err := orasoci.New(layoutDir)
	if err != nil {
		return ocispec.Descriptor{}, fmt.Errorf("open oci layout %s: %w", layoutDir, err)
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)

	layers := make([]ocispec.Descriptor, 0, len(names))
	for _, name := range names {
		data := files[name]
		desc := content.NewDescriptorFromBytes("application/octet-stream", data)
		desc.Annotations = map[string]string{ocispec.AnnotationTitle: name}
		if err := store.Push(ctx, desc, bytes.NewReader(data)); err != nil {
			return ocispec.Descriptor{}, fmt.Errorf("push layer %s: %w", name, err)
		}
		layers = append(layers, desc)
	}

	manifestDesc, err := oras.PackManifest(ctx, store, oras.PackManifestVersion1_1, artifactType, oras.PackManifestOptions{
		Layers: layers,
		ManifestAnnotations: map[string]string{
			ocispec.AnnotationCreated: "2026-11-27T00:00:00Z",
		},
	})
	if err != nil {
		return ocispec.Descriptor{}, fmt.Errorf("pack oci manifest: %w", err)
	}
	if err := store.Tag(ctx, manifestDesc, tag); err != nil {
		return ocispec.Descriptor{}, fmt.Errorf("tag oci manifest %s: %w", tag, err)
	}
	return manifestDesc, nil
}

// CopyBetweenLayouts synchronizes a multi-blob OCI artifact graph across two OCI layouts via oras.Copy (T-M1-16).
func CopyBetweenLayouts(ctx context.Context, srcDir, srcTag, dstDir, dstTag string) (ocispec.Descriptor, error) {
	src, err := orasoci.New(srcDir)
	if err != nil {
		return ocispec.Descriptor{}, err
	}
	dst, err := orasoci.New(dstDir)
	if err != nil {
		return ocispec.Descriptor{}, err
	}
	return oras.Copy(ctx, src, srcTag, dst, dstTag, oras.DefaultCopyOptions)
}

// ExtractFile reads a named layer file from an OCI layout tag with content-addressable digest verification.
func ExtractFile(ctx context.Context, layoutDir, tag, fileName string) ([]byte, ocispec.Descriptor, error) {
	store, err := orasoci.New(layoutDir)
	if err != nil {
		return nil, ocispec.Descriptor{}, err
	}
	rootDesc, err := store.Resolve(ctx, tag)
	if err != nil {
		return nil, ocispec.Descriptor{}, fmt.Errorf("resolve oci tag %q: %w", tag, err)
	}
	rc, err := store.Fetch(ctx, rootDesc)
	if err != nil {
		return nil, ocispec.Descriptor{}, err
	}
	rawManifest, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil {
		return nil, ocispec.Descriptor{}, err
	}
	var mf ocispec.Manifest
	if err := json.Unmarshal(rawManifest, &mf); err != nil {
		return nil, ocispec.Descriptor{}, err
	}
	for _, layer := range mf.Layers {
		if layer.Annotations[ocispec.AnnotationTitle] == fileName {
			lrc, lerr := store.Fetch(ctx, layer)
			if lerr != nil {
				return nil, ocispec.Descriptor{}, lerr
			}
			data, rerr := io.ReadAll(lrc)
			_ = lrc.Close()
			if rerr != nil {
				return nil, ocispec.Descriptor{}, rerr
			}
			return data, layer, nil
		}
	}
	return nil, ocispec.Descriptor{}, fmt.Errorf("file %q not found in oci tag %q", fileName, tag)
}
