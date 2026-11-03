package deploy

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v4"
)

const localOverlay = "k8s/overlays/local/kustomization.yaml"

type overlayImages struct {
	Images []struct {
		Name    string `yaml:"name"`
		NewName string `yaml:"newName"`
		NewTag  string `yaml:"newTag"`
	} `yaml:"images"`
}

func TestK8sLoadBuildsImagesTheLocalOverlayExpects(t *testing.T) {
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("make not installed")
	}

	raw, err := os.ReadFile(localOverlay)
	require.NoError(t, err)
	var overlay overlayImages
	require.NoError(t, yaml.Load(raw, &overlay))
	require.NotEmpty(t, overlay.Images, "local overlay must pin images")
	for _, img := range overlay.Images {
		require.Empty(t, img.NewName,
			"%s uses newName; this test compares name:newTag and would pass wrongly — render `kubectl kustomize` and compare Deployment images instead", img.Name)
	}

	fakeBin := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(fakeBin, "minikube"), []byte("#!/bin/sh\nexit 0\n"), 0o755))

	cmd := exec.Command("make", "-n", "-C", "..", "k8s-load")
	cmd.Env = append(os.Environ(), "PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))

	for _, img := range overlay.Images {
		want := "-t " + img.Name + ":" + img.NewTag
		require.True(t, strings.Contains(string(out), want),
			"make k8s-load must build %q (from %s), otherwise pods hit ErrImageNeverPull\n%s", want, localOverlay, out)
	}
}
