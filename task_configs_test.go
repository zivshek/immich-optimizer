package main

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestTaskConfigRegistrySelectsImageAndVideoConfigsIndependently(t *testing.T) {
	root := t.TempDir()
	writeTestTaskConfig(t, root, "standard/alpha", []byte(`
tasks:
  - name: images
    command: ""
    extensions: [jpg]
  - name: videos
    command: ""
    extensions: [mp4]
`))
	writeTestTaskConfig(t, root, "standard/zeta", []byte(`
tasks:
  - name: videos
    command: ""
    extensions: [mp4]
`))
	customRoot := t.TempDir()
	writeTestTaskConfig(t, customRoot, "my-profile", []byte(`
tasks:
  - name: images
    command: ""
    extensions: [jpg]
`))

	store, err := NewStatsStore(filepath.Join(t.TempDir(), "stats.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	configFile := filepath.Join(root, "standard", "alpha", "tasks.yaml")
	startupConfig, err := NewConfig(&configFile)
	if err != nil {
		t.Fatal(err)
	}
	profile := &ProfileConfig{Name: "alice", ConfigFile: configFile, Tasks: startupConfig}
	watcher := &FileWatcher{
		profile: profile, config: profile.Tasks, logger: log.New(io.Discard, "", 0),
		imageConfig: taskConfigSelection{Config: profile.Tasks, File: profile.ConfigFile, Name: profile.ConfigFile},
		videoConfig: taskConfigSelection{Config: profile.Tasks, File: profile.ConfigFile, Name: profile.ConfigFile},
	}
	registry := NewTaskConfigRegistry(root, customRoot, store)
	if err := registry.Register(watcher); err != nil {
		t.Fatal(err)
	}

	profiles, err := registry.List()
	if err != nil {
		t.Fatal(err)
	}
	if profiles[0].ImageCurrent != "standard/alpha" || profiles[0].VideoCurrent != "standard/alpha" {
		t.Fatalf("unexpected initial configs: %+v", profiles)
	}
	if len(profiles[0].ImageConfigs) != 2 || profiles[0].ImageConfigs[0].Name != "custom/my-profile" {
		t.Fatalf("unexpected image configs: %+v", profiles[0].ImageConfigs)
	}
	if len(profiles[0].VideoConfigs) != 2 || profiles[0].VideoConfigs[0].Name != "standard/alpha" {
		t.Fatalf("unexpected video configs: %+v", profiles[0].VideoConfigs)
	}

	if err := registry.Select("alice", mediaTypeImage, "custom/my-profile"); err != nil {
		t.Fatal(err)
	}
	if err := registry.Select("alice", mediaTypeVideo, "standard/zeta"); err != nil {
		t.Fatal(err)
	}
	if err := registry.SetNvidia("alice", true); err != nil {
		t.Fatal(err)
	}
	if err := registry.SetImageScore("alice", 87); err != nil {
		t.Fatal(err)
	}
	if err := registry.SetVideoScore("alice", 93); err != nil {
		t.Fatal(err)
	}
	if err := registry.SetVideoCRF("alice", 30); err != nil {
		t.Fatal(err)
	}
	profiles, err = registry.List()
	if err != nil {
		t.Fatal(err)
	}
	if profiles[0].ImageCurrent != "custom/my-profile" || profiles[0].VideoCurrent != "standard/zeta" {
		t.Fatalf("independent selections were not applied: %+v", profiles)
	}
	if !profiles[0].UseNvidia {
		t.Fatal("NVIDIA option was not applied")
	}
	if profiles[0].ImageScore != 87 {
		t.Fatalf("image score was not applied: %d", profiles[0].ImageScore)
	}
	if profiles[0].VideoScore != 93 {
		t.Fatalf("video score was not applied: %d", profiles[0].VideoScore)
	}
	if profiles[0].VideoCRF != 30 {
		t.Fatalf("video CRF was not applied: %d", profiles[0].VideoCRF)
	}
	if err := registry.SetVideoCRF("alice", 64); err == nil {
		t.Fatal("video CRF above 63 was accepted")
	}
	imageSelected, err := store.SelectedMediaTaskConfig("alice", mediaTypeImage)
	if err != nil || imageSelected != "custom/my-profile" {
		t.Fatalf("image selection was not persisted: %q, %v", imageSelected, err)
	}
	videoSelected, err := store.SelectedMediaTaskConfig("alice", mediaTypeVideo)
	if err != nil || videoSelected != "standard/zeta" {
		t.Fatalf("video selection was not persisted: %q, %v", videoSelected, err)
	}
}

func TestBundledPerceptualConfigsAreSinglePurpose(t *testing.T) {
	root := filepath.Join("config", "perceptual")
	paths := make(map[string]string)
	if err := discoverTaskConfigPaths(root, "perceptual", paths); err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(paths))
	for name, path := range paths {
		config, err := NewConfig(&path)
		if err != nil {
			t.Fatalf("load %s: %v", name, err)
		}
		hasImages, hasVideos := configMediaTypes(config)
		if hasImages == hasVideos {
			t.Fatalf("%s must contain exactly one media type", name)
		}
		names = append(names, name)
	}
	sort.Strings(names)
	expected := []string{"perceptual/av1", "perceptual/avif", "perceptual/hevc", "perceptual/webp"}
	if len(names) != len(expected) {
		t.Fatalf("unexpected perceptual configs: %v", names)
	}
	for i := range expected {
		if names[i] != expected[i] {
			t.Fatalf("unexpected perceptual configs: %v", names)
		}
	}
}

func TestStandardAv1UsesFixedCrfAndPerceptualAv1UsesShorterSamples(t *testing.T) {
	standardScript, err := os.ReadFile(
		filepath.Join("config", "standard", "av1-crf28", "transcode-video.sh"),
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"-c:v libsvtav1", "-preset 6", `crf="${IUO_VIDEO_CRF:-28}"`, `-crf "$crf"`, "-noautorotate"} {
		if !strings.Contains(string(standardScript), expected) {
			t.Fatalf("standard AV1 script is missing %q", expected)
		}
	}
	if strings.Contains(string(standardScript), "auto-encode") {
		t.Fatal("standard AV1 script must not use perceptual auto-encode")
	}

	perceptualScript, err := os.ReadFile(
		filepath.Join("config", "perceptual", "av1", "transcode-video.sh"),
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"--min-samples 5", "--sample-every 2m", "--sample-duration 6s"} {
		if !strings.Contains(string(perceptualScript), expected) {
			t.Fatalf("perceptual AV1 script is missing %q", expected)
		}
	}
}

func TestPerceptualImagesUseDashboardSsimulacra2Target(t *testing.T) {
	avifScript, err := os.ReadFile(filepath.Join("config", "perceptual", "avif", "transcode-image.sh"))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		`target_score="${IUO_IMAGE_SCORE:-85}"`,
		"magick",
		"fssimu2",
		"AVIF quality",
		"selected AVIF quality",
	} {
		if !strings.Contains(string(avifScript), expected) {
			t.Fatalf("perceptual AVIF script is missing %q", expected)
		}
	}
	if strings.Contains(string(avifScript), "oavif") {
		t.Fatal("perceptual AVIF script must not use the broken oavif encoder path")
	}

	webpScript, err := os.ReadFile(filepath.Join("config", "perceptual", "webp", "transcode-image.sh"))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`target_score="${IUO_IMAGE_SCORE:-85}"`, "fssimu2", "2>&1", "unable to read SSIMULACRA2 score", "selected WebP quality"} {
		if !strings.Contains(string(webpScript), expected) {
			t.Fatalf("perceptual WebP script is missing %q", expected)
		}
	}
}

func TestPerceptualVideosUseDashboardVmafTarget(t *testing.T) {
	for _, path := range []string{
		filepath.Join("config", "perceptual", "av1", "transcode-video.sh"),
		filepath.Join("config", "perceptual", "hevc", "transcode-video-cpu.sh"),
		filepath.Join("config", "perceptual", "hevc", "transcode-video-nvidia.sh"),
	} {
		script, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(script), `IUO_VIDEO_SCORE:-95`) {
			t.Fatalf("%s does not use the dashboard VMAF target", path)
		}
	}
}

func TestTaskConfigRegistryMigratesLegacyNvidiaPerceptualProfile(t *testing.T) {
	root := t.TempDir()
	writeTestTaskConfig(t, root, "perceptual/webp", []byte(`
tasks:
  - name: images
    command: ""
    extensions: [jpg]
`))
	writeTestTaskConfig(t, root, "perceptual/hevc", []byte(`
tasks:
  - name: videos
    command: ""
    extensions: [mp4]
`))
	hevcPath := filepath.Join(root, "perceptual", "hevc", "tasks.yaml")
	hevcConfig, err := NewConfig(&hevcPath)
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewStatsStore(filepath.Join(t.TempDir(), "stats.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	profile := &ProfileConfig{
		Name:                 "alice",
		ConfigFile:           hevcPath,
		Tasks:                hevcConfig,
		LegacyTaskConfigName: "perceptual/perceptual-hevc-webp-nvidia",
		LegacyUseNvidia:      true,
	}
	watcher := &FileWatcher{profile: profile, config: hevcConfig, logger: log.New(io.Discard, "", 0)}
	registry := NewTaskConfigRegistry(root, filepath.Join(t.TempDir(), "missing-custom"), store)
	if err := registry.Register(watcher); err != nil {
		t.Fatal(err)
	}
	profiles, err := registry.List()
	if err != nil {
		t.Fatal(err)
	}
	if profiles[0].ImageCurrent != "perceptual/webp" || profiles[0].VideoCurrent != "perceptual/hevc" || !profiles[0].UseNvidia {
		t.Fatalf("legacy profile was not migrated: %+v", profiles[0])
	}
}

func writeTestTaskConfig(t *testing.T, root, name string, contents []byte) {
	t.Helper()
	dir := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(dir, 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tasks.yaml"), contents, 0600); err != nil {
		t.Fatal(err)
	}
}
