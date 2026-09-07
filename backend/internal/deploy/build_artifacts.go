package deploy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

var (
	ErrUnsupportedBuilder = errors.New("unsupported deployment builder")
	ErrBuilderUnavailable = errors.New("deployment builder is unavailable")
	ErrArtifactMissing    = errors.New("deployment artifact is missing")
	ErrArtifactRetained   = errors.New("deployment artifact is retained")
)

const AutomaticRecipeVersion = "just-dashboard-recipes-v1"

// The catalogue is deliberately small and reviewed. Tags are never written
// into a release Dockerfile: the backend resolves each to a digest first.
var recipeBaseCatalogue = map[string][]string{
	"node:npm":  {"node:22-alpine"},
	"node:pnpm": {"node:22-alpine"},
	"node:yarn": {"node:22-alpine"},
	"node:bun":  {"oven/bun:1-alpine"},
	"go":        {"golang:1.25-alpine", "alpine:3.22"},
	"python":    {"python:3.13-slim"},
	"static":    {"nginx:1.29-alpine"},
}

type ResolvedImage struct {
	Reference    string   `json:"reference"`
	Digest       string   `json:"digest"`
	ConfigDigest string   `json:"configDigest,omitempty"`
	OS           string   `json:"os,omitempty"`
	Architecture string   `json:"architecture,omitempty"`
	Platforms    []string `json:"platforms"`
	SizeBytes    int64    `json:"sizeBytes,omitempty"`
}

type BuildLog struct {
	Stream string
	Text   string
}

type BuildInvocation struct {
	ContextDir string
	Dockerfile string
	Tag        string
	Platform   string
	NoCache    bool
	Pull       bool
	Secrets    []BuildSecretValue
}

type BuildSecretValue struct {
	ID    string
	Step  string
	Value string
}

type BuildBackend interface {
	ResolveImage(context.Context, string, string) (ResolvedImage, error)
	BuildImage(context.Context, BuildInvocation, func(BuildLog) error) (ResolvedImage, error)
	PullImage(context.Context, string, string, func(BuildLog) error) (ResolvedImage, error)
	InspectImage(context.Context, string) (ResolvedImage, error)
	RemoveImage(context.Context, string) error
}

type PreparedBuild struct {
	Method               BuildMethod              `json:"method"`
	Recipe               string                   `json:"recipe,omitempty"`
	RecipeVersion        string                   `json:"recipeVersion,omitempty"`
	Dockerfile           string                   `json:"dockerfile,omitempty"`
	DockerfileDigest     string                   `json:"dockerfileDigest,omitempty"`
	DockerfilePreview    string                   `json:"dockerfilePreview,omitempty"`
	BuildArgv            []string                 `json:"buildArgv"`
	BaseImages           []ResolvedImage          `json:"baseImages"`
	TargetPlatform       string                   `json:"targetPlatform,omitempty"`
	CachePolicy          string                   `json:"cachePolicy"`
	SecretIDs            []string                 `json:"secretIds"`
	SecretLayerGuarantee string                   `json:"secretLayerGuarantee"`
	ComposeServices      []PreparedComposeService `json:"composeServices,omitempty"`
}

type PreparedComposeService struct {
	Name             string   `json:"name"`
	BuildContext     string   `json:"buildContext"`
	Dockerfile       string   `json:"dockerfile"`
	DockerfileDigest string   `json:"dockerfileDigest"`
	BuildArgv        []string `json:"buildArgv"`
}

type BuildArtifactResult struct {
	Artifacts []ReleaseArtifactInput   `json:"artifacts"`
	Image     ResolvedImage            `json:"image"`
	Compose   *ResolvedComposeSnapshot `json:"compose,omitempty"`
	Prepared  PreparedBuild            `json:"prepared"`
}

type ResolvedComposeSnapshot struct {
	SourceDigest string                   `json:"sourceDigest"`
	Files        []string                 `json:"files"`
	Services     []ResolvedComposeService `json:"services"`
}

type ResolvedComposeService struct {
	Plan         ComposeServicePlan `json:"plan"`
	Reference    string             `json:"reference"`
	Digest       string             `json:"digest"`
	ConfigDigest string             `json:"configDigest,omitempty"`
	Source       string             `json:"source"`
}

type ArtifactBuilder struct{ backend BuildBackend }

func NewArtifactBuilder(backend BuildBackend) *ArtifactBuilder {
	return &ArtifactBuilder{backend: backend}
}

func (b *ArtifactBuilder) Prepare(
	ctx context.Context,
	root string,
	config BuildPlanConfig,
	forceNoCache bool,
	tag string,
) (PreparedBuild, error) {
	prepared := PreparedBuild{
		Method: config.Method, Recipe: config.Recipe, BuildArgv: []string{},
		BaseImages: []ResolvedImage{}, TargetPlatform: strings.ToLower(config.TargetPlatform),
		CachePolicy: "reuse", SecretIDs: []string{}, SecretLayerGuarantee: "not_applicable",
	}
	if config.NoCache || forceNoCache {
		prepared.CachePolicy = "no_cache"
	}
	for _, secret := range config.Secrets {
		prepared.SecretIDs = append(prepared.SecretIDs, secret.Variable)
	}
	sort.Strings(prepared.SecretIDs)

	switch config.Method {
	case BuildRecipe:
		if b.backend == nil {
			return PreparedBuild{}, ErrBuilderUnavailable
		}
		recipe, err := selectRecipe(root, config)
		if err != nil {
			return PreparedBuild{}, err
		}
		prepared.Recipe = recipe.kind
		prepared.RecipeVersion = AutomaticRecipeVersion
		baseRefs := append([]string(nil), recipeBaseCatalogue[recipe.catalogueKey]...)
		if recipe.kind == "node" && config.OutputDirectory != "" {
			baseRefs = append(baseRefs, recipeBaseCatalogue["static"]...)
		}
		bases, err := b.resolveBases(ctx, baseRefs)
		if err != nil {
			return PreparedBuild{}, err
		}
		prepared.BaseImages = bases
		content, err := renderRecipeDockerfile(recipe, config, bases)
		if err != nil {
			return PreparedBuild{}, err
		}
		prepared.SecretLayerGuarantee = "buildkit_ephemeral_mount"
		if err := writeGeneratedDockerfile(root, content); err != nil {
			return PreparedBuild{}, err
		}
		prepared.Dockerfile = ".just-dashboard/Dockerfile"
		prepared.DockerfilePreview = content
		prepared.DockerfileDigest = digestText(content)
	case BuildStatic:
		if b.backend == nil {
			return PreparedBuild{}, ErrBuilderUnavailable
		}
		bases, err := b.resolveBases(ctx, recipeBaseCatalogue["static"])
		if err != nil {
			return PreparedBuild{}, err
		}
		prepared.BaseImages = bases
		content, err := renderStaticDockerfile(config, bases[0])
		if err != nil {
			return PreparedBuild{}, err
		}
		if err := writeGeneratedDockerfile(root, content); err != nil {
			return PreparedBuild{}, err
		}
		prepared.Dockerfile = ".just-dashboard/Dockerfile"
		prepared.DockerfilePreview = content
		prepared.DockerfileDigest = digestText(content)
	case BuildDockerfile:
		dockerfile := config.Dockerfile
		if dockerfile == "" {
			dockerfile = "Dockerfile"
		}
		content, err := readContainedRegular(root, dockerfile, 2<<20)
		if err != nil {
			return PreparedBuild{}, fmt.Errorf("%w: Dockerfile: %v", ErrUnsupportedBuilder, err)
		}
		if err := validateCustomDockerfile(content); err != nil {
			return PreparedBuild{}, err
		}
		prepared.Dockerfile = filepath.ToSlash(filepath.Clean(dockerfile))
		prepared.DockerfilePreview = string(content)
		prepared.DockerfileDigest = digestBytes(content)
		if len(config.Secrets) != 0 {
			return PreparedBuild{}, fmt.Errorf("%w: custom Dockerfiles cannot prove scoped secrets remain out of layers; use a reviewed recipe", ErrUnsupportedBuilder)
		}
	case BuildImage, BuildCompose, BuildNone, BuildLegacyCompose:
		return prepared, nil
	default:
		return PreparedBuild{}, ErrUnsupportedBuilder
	}

	prepared.BuildArgv = buildPreviewArgv(prepared, tag)
	return prepared, nil
}

func (b *ArtifactBuilder) Build(
	ctx context.Context,
	root, tag string,
	config BuildPlanConfig,
	prepared PreparedBuild,
	variables map[string]string,
	registryAuth string,
	source SourceIdentity,
	compose *ComposeAnalysis,
	emit func(BuildLog) error,
) (BuildArtifactResult, error) {
	result := BuildArtifactResult{Prepared: prepared, Artifacts: []ReleaseArtifactInput{}}
	if emit == nil {
		emit = func(BuildLog) error { return nil }
	}
	if (b == nil || b.backend == nil) && config.Method != BuildNone {
		return result, ErrBuilderUnavailable
	}
	switch config.Method {
	case BuildRecipe, BuildDockerfile, BuildStatic:
		secrets := make([]BuildSecretValue, 0, len(config.Secrets))
		for _, requested := range config.Secrets {
			value, ok := variables[requested.Variable]
			if !ok {
				return result, fmt.Errorf("%w: build variable %s is unavailable", ErrArtifactMissing, requested.Variable)
			}
			secrets = append(secrets, BuildSecretValue{ID: requested.Variable, Step: requested.Step, Value: value})
		}
		image, err := b.backend.BuildImage(ctx, BuildInvocation{
			ContextDir: root, Dockerfile: prepared.Dockerfile, Tag: tag,
			Platform: prepared.TargetPlatform, NoCache: prepared.CachePolicy == "no_cache",
			Pull: true, Secrets: secrets,
		}, redactBuildEmitter(variables, emit))
		if err != nil {
			return result, err
		}
		if err := validateResolvedImage(image); err != nil {
			return result, err
		}
		result.Image = image
		result.Artifacts = append(result.Artifacts, imageArtifact(image, prepared))
		if config.Method == BuildStatic || (config.Method == BuildRecipe && config.OutputDirectory != "") {
			result.Artifacts = append(result.Artifacts, ReleaseArtifactInput{
				Kind: ArtifactStaticBundle, Reference: config.OutputDirectory,
				Digest:   prepared.DockerfileDigest,
				Metadata: mustJSON(map[string]any{"packagedInImage": image.Digest}),
			})
		}
	case BuildImage:
		if source.Digest == "" || source.Repository == "" {
			return result, fmt.Errorf("%w: image source has no immutable digest", ErrArtifactMissing)
		}
		exact := source.Repository + "@" + source.Digest
		image, err := b.backend.PullImage(ctx, exact, registryAuth, redactBuildEmitter(variables, emit))
		if err != nil {
			return result, err
		}
		if image.Digest == "" {
			image.Digest = source.Digest
		}
		if err := validateResolvedImage(image); err != nil {
			return result, err
		}
		result.Image = image
		result.Artifacts = append(result.Artifacts, imageArtifact(image, prepared))
	case BuildCompose:
		if compose == nil {
			return result, fmt.Errorf("%w: Compose analysis is unavailable", ErrArtifactMissing)
		}
		resolved := &ResolvedComposeSnapshot{
			SourceDigest: compose.Digest, Files: append([]string(nil), compose.Files...),
			Services: []ResolvedComposeService{},
		}
		for _, service := range compose.Services {
			if service.BuildContext != "" {
				if strings.Contains(service.BuildContext, "$") || strings.Contains(service.BuildDockerfile, "$") {
					return result, fmt.Errorf("%w: Compose service %s uses a dynamic build path", ErrUnsupportedBuilder, service.Name)
				}
				contextRoot, err := containedSubdirectory(root, service.BuildContext)
				if err != nil {
					return result, fmt.Errorf("%w: Compose service %s build context: %v", ErrUnsupportedBuilder, service.Name, err)
				}
				dockerfile := service.BuildDockerfile
				if dockerfile == "" {
					dockerfile = "Dockerfile"
				}
				content, err := readContainedRegular(contextRoot, dockerfile, 2<<20)
				if err != nil {
					return result, fmt.Errorf("%w: Compose service %s Dockerfile: %v", ErrUnsupportedBuilder, service.Name, err)
				}
				if err := validateCustomDockerfile(content); err != nil {
					return result, fmt.Errorf("Compose service %s: %w", service.Name, err)
				}
				serviceTag := composeServiceImageTag(tag, service.Name)
				servicePrepared := PreparedBuild{
					Method: BuildDockerfile, Dockerfile: dockerfile, DockerfileDigest: digestBytes(content),
					TargetPlatform: prepared.TargetPlatform, CachePolicy: prepared.CachePolicy,
				}
				servicePrepared.BuildArgv = buildPreviewArgv(servicePrepared, serviceTag)
				image, err := b.backend.BuildImage(ctx, BuildInvocation{
					ContextDir: contextRoot, Dockerfile: dockerfile, Tag: serviceTag,
					Platform: prepared.TargetPlatform, NoCache: prepared.CachePolicy == "no_cache", Pull: true,
				}, redactBuildEmitter(variables, emit))
				if err != nil {
					return result, fmt.Errorf("build Compose service %s: %w", service.Name, err)
				}
				if err := validateResolvedImage(image); err != nil {
					return result, err
				}
				result.Prepared.ComposeServices = append(result.Prepared.ComposeServices, PreparedComposeService{
					Name: service.Name, BuildContext: service.BuildContext, Dockerfile: dockerfile,
					DockerfileDigest: servicePrepared.DockerfileDigest, BuildArgv: servicePrepared.BuildArgv,
				})
				resolved.Services = append(resolved.Services, composeResolvedService(service, image, "build"))
				result.Artifacts = append(result.Artifacts, composeImageArtifact(service.Name, image, servicePrepared, "build"))
				continue
			}
			if service.Image == "" || strings.Contains(service.Image, "${") {
				return result, fmt.Errorf("%w: Compose service %s has no immutable image or supported build", ErrUnsupportedBuilder, service.Name)
			}
			image, err := b.backend.ResolveImage(ctx, service.Image, registryAuth)
			if err != nil {
				return result, fmt.Errorf("resolve Compose service %s image: %w", service.Name, err)
			}
			if err := validateResolvedImage(image); err != nil {
				return result, err
			}
			pulled, err := b.backend.PullImage(ctx, image.Reference+"@"+image.Digest, registryAuth, redactBuildEmitter(variables, emit))
			if err != nil {
				return result, fmt.Errorf("pull Compose service %s image: %w", service.Name, err)
			}
			if err := validateResolvedImage(pulled); err != nil || pulled.Digest != image.Digest {
				return result, fmt.Errorf("%w: Compose service %s pull did not preserve its resolved digest", ErrArtifactMissing, service.Name)
			}
			resolved.Services = append(resolved.Services, composeResolvedService(service, pulled, "pull"))
			result.Artifacts = append(result.Artifacts, composeImageArtifact(service.Name, pulled, prepared, "pull"))
		}
		result.Compose = resolved
		metadata := mustJSON(resolved)
		result.Artifacts = append(result.Artifacts, ReleaseArtifactInput{
			Kind: ArtifactCompose, Reference: "compose", Digest: digestBytes(metadata), Metadata: metadata,
		})
	case BuildNone:
		return result, nil
	case BuildLegacyCompose:
		return result, fmt.Errorf("%w: legacy Compose runs use the compatibility executor", ErrUnsupportedBuilder)
	default:
		return result, ErrUnsupportedBuilder
	}
	return result, nil
}

type selectedRecipe struct {
	kind, catalogueKey, lockfile string
	mainPackage                  string
}

func selectRecipe(root string, config BuildPlanConfig) (selectedRecipe, error) {
	requested := config.Recipe
	if requested == "" {
		switch {
		case regularExists(root, "package.json"):
			requested = "node"
		case regularExists(root, "go.mod"):
			requested = "go"
		case regularExists(root, "uv.lock") || regularExists(root, "poetry.lock") || regularExists(root, "requirements.txt"):
			requested = "python"
		}
	}
	switch requested {
	case "node":
		locks := []struct{ path, manager string }{
			{"bun.lock", "bun"}, {"bun.lockb", "bun"}, {"package-lock.json", "npm"},
			{"pnpm-lock.yaml", "pnpm"}, {"yarn.lock", "yarn"},
		}
		found := []struct{ path, manager string }{}
		for _, lock := range locks {
			if regularExists(root, lock.path) {
				found = append(found, lock)
			}
		}
		if len(found) != 1 {
			return selectedRecipe{}, fmt.Errorf("%w: Node recipes require exactly one supported lockfile; found %d", ErrUnsupportedBuilder, len(found))
		}
		return selectedRecipe{kind: "node", catalogueKey: "node:" + found[0].manager, lockfile: found[0].path}, nil
	case "go":
		if !regularExists(root, "go.mod") {
			return selectedRecipe{}, fmt.Errorf("%w: Go recipe requires go.mod", ErrUnsupportedBuilder)
		}
		mains, err := findGoMainPackages(root, 10_000)
		if err != nil {
			return selectedRecipe{}, err
		}
		if len(mains) != 1 {
			return selectedRecipe{}, fmt.Errorf("%w: Go recipe requires exactly one detected main package; found %d", ErrUnsupportedBuilder, len(mains))
		}
		return selectedRecipe{kind: "go", catalogueKey: "go", mainPackage: mains[0]}, nil
	case "python":
		lock, err := selectPythonLock(root)
		if err != nil {
			return selectedRecipe{}, err
		}
		if strings.TrimSpace(config.StartCommand) == "" {
			return selectedRecipe{}, fmt.Errorf("%w: Python recipe requires an explicit ASGI/WSGI start command", ErrUnsupportedBuilder)
		}
		return selectedRecipe{kind: "python", catalogueKey: "python", lockfile: lock}, nil
	default:
		return selectedRecipe{}, fmt.Errorf("%w: no supported automatic recipe was selected", ErrUnsupportedBuilder)
	}
}

func renderRecipeDockerfile(recipe selectedRecipe, config BuildPlanConfig, bases []ResolvedImage) (string, error) {
	if len(bases) == 0 {
		return "", ErrBuilderUnavailable
	}
	installSecrets := buildSecretMounts(config.Secrets, "install")
	buildSecrets := buildSecretMounts(config.Secrets, "build")
	var lines []string
	lines = append(lines, "# syntax=docker/dockerfile:1.10")
	switch recipe.kind {
	case "node":
		base := immutableImageReference(bases[0])
		lines = append(lines, "FROM "+base+" AS build", "WORKDIR /app", "COPY . .")
		manager := strings.TrimPrefix(recipe.catalogueKey, "node:")
		install := map[string]string{
			"npm": "npm ci", "pnpm": "corepack enable && pnpm install --frozen-lockfile",
			"yarn": "corepack enable && yarn install --immutable", "bun": "bun install --frozen-lockfile",
		}[manager]
		lines = append(lines, "RUN "+installSecrets+install)
		if command := strings.TrimSpace(config.BuildCommand); command != "" {
			lines = append(lines, "RUN "+buildSecrets+command)
		}
		if output := strings.TrimSpace(config.OutputDirectory); output != "" {
			static, err := resolveCatalogueImage(bases, recipeBaseCatalogue["static"][0])
			if err != nil {
				return "", err
			}
			lines = append(lines, "FROM "+immutableImageReference(static), "COPY --from=build /app/"+filepath.ToSlash(output)+"/ /usr/share/nginx/html/")
		} else {
			if strings.TrimSpace(config.StartCommand) == "" {
				return "", fmt.Errorf("%w: Node service recipe requires a start command", ErrUnsupportedBuilder)
			}
			lines = append(lines, "FROM "+base, "WORKDIR /app", "ENV NODE_ENV=production", "COPY --from=build /app /app", shellCMD(config.StartCommand))
		}
	case "go":
		if len(bases) != 2 {
			return "", ErrBuilderUnavailable
		}
		packagePath := "./"
		if recipe.mainPackage != "." {
			packagePath += filepath.ToSlash(recipe.mainPackage)
		}
		lines = append(lines,
			"FROM "+immutableImageReference(bases[0])+" AS build", "WORKDIR /src", "COPY . .",
			"RUN "+installSecrets+"go mod download",
			"RUN "+buildSecrets+"CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/app "+packagePath,
			"FROM "+immutableImageReference(bases[1]), "RUN adduser -D -u 10001 app", "USER app", "COPY --from=build /out/app /app", `ENTRYPOINT ["/app"]`,
		)
	case "python":
		base := immutableImageReference(bases[0])
		lines = append(lines, "FROM "+base, "WORKDIR /app", "COPY . .")
		switch recipe.lockfile {
		case "requirements.txt":
			lines = append(lines, "RUN "+installSecrets+"pip install --no-cache-dir --requirement requirements.txt")
		case "uv.lock":
			lines = append(lines, "RUN "+installSecrets+"pip install --no-cache-dir uv && uv sync --frozen --no-dev")
		case "poetry.lock":
			lines = append(lines, "RUN "+installSecrets+"pip install --no-cache-dir poetry && poetry install --only main --no-root --no-interaction")
		}
		if command := strings.TrimSpace(config.BuildCommand); command != "" {
			lines = append(lines, "RUN "+buildSecrets+command)
		}
		lines = append(lines, shellCMD(config.StartCommand))
	default:
		return "", ErrUnsupportedBuilder
	}
	return strings.Join(lines, "\n") + "\n", nil
}

func renderStaticDockerfile(config BuildPlanConfig, base ResolvedImage) (string, error) {
	output := strings.TrimSpace(config.OutputDirectory)
	if output == "" {
		output = "."
	} else if !safeRelativePath(output) {
		return "", fmt.Errorf("%w: static output directory is invalid", ErrUnsupportedBuilder)
	}
	return "# syntax=docker/dockerfile:1.10\nFROM " + immutableImageReference(base) +
		"\nCOPY " + filepath.ToSlash(output) + "/ /usr/share/nginx/html/\n", nil
}

func (b *ArtifactBuilder) resolveBases(ctx context.Context, references []string) ([]ResolvedImage, error) {
	resolved := make([]ResolvedImage, 0, len(references)+1)
	for _, reference := range references {
		image, err := b.backend.ResolveImage(ctx, reference, "")
		if err != nil {
			return nil, fmt.Errorf("%w: resolve reviewed base image %s: %v", ErrBuilderUnavailable, reference, err)
		}
		if err := validateResolvedImage(image); err != nil {
			return nil, err
		}
		resolved = append(resolved, image)
	}
	return resolved, nil
}

func buildPreviewArgv(prepared PreparedBuild, tag string) []string {
	argv := []string{"docker", "buildx", "build", "--progress=plain", "--file", prepared.Dockerfile, "--tag", tag, "--load", "--pull"}
	if prepared.TargetPlatform != "" {
		argv = append(argv, "--platform", prepared.TargetPlatform)
	}
	if prepared.CachePolicy == "no_cache" {
		argv = append(argv, "--no-cache")
	}
	for _, id := range prepared.SecretIDs {
		argv = append(argv, "--secret", "id="+id+",env=<ephemeral>")
	}
	return append(argv, ".")
}

func buildSecretMounts(secrets []BuildSecretConfig, step string) string {
	ids := []string{}
	for _, secret := range secrets {
		if secret.Step == step {
			ids = append(ids, secret.Variable)
		}
	}
	sort.Strings(ids)
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, "--mount=type=secret,id="+id+",env="+id)
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " ") + " "
}

func shellCMD(command string) string {
	encoded, _ := json.Marshal([]string{"/bin/sh", "-c", strings.TrimSpace(command)})
	return "CMD " + string(encoded)
}

func immutableImageReference(image ResolvedImage) string {
	return image.Reference + "@" + image.Digest
}

func resolveCatalogueImage(images []ResolvedImage, reference string) (ResolvedImage, error) {
	for _, image := range images {
		if image.Reference == reference {
			return image, nil
		}
	}
	return ResolvedImage{}, fmt.Errorf("%w: reviewed base %s was not resolved", ErrBuilderUnavailable, reference)
}

func validateResolvedImage(image ResolvedImage) error {
	if image.Reference == "" || !contentDigestRE.MatchString(image.Digest) ||
		strings.ContainsAny(image.Reference, "\x00\r\n") {
		return fmt.Errorf("%w: backend returned no immutable image identity", ErrArtifactMissing)
	}
	return nil
}

func imageArtifact(image ResolvedImage, prepared PreparedBuild) ReleaseArtifactInput {
	metadata := mustJSON(map[string]any{
		"configDigest": image.ConfigDigest, "os": image.OS, "architecture": image.Architecture,
		"platforms": image.Platforms, "dockerfileDigest": prepared.DockerfileDigest,
	})
	return ReleaseArtifactInput{
		Kind: ArtifactImage, Reference: image.Reference, Digest: image.Digest,
		Metadata: metadata, SizeBytes: image.SizeBytes,
	}
}

func composeResolvedService(plan ComposeServicePlan, image ResolvedImage, source string) ResolvedComposeService {
	return ResolvedComposeService{
		Plan: plan, Reference: image.Reference, Digest: image.Digest,
		ConfigDigest: image.ConfigDigest, Source: source,
	}
}

func composeImageArtifact(service string, image ResolvedImage, prepared PreparedBuild, source string) ReleaseArtifactInput {
	artifact := imageArtifact(image, prepared)
	artifact.Metadata = mustJSON(map[string]any{
		"service": service, "source": source, "configDigest": image.ConfigDigest,
		"os": image.OS, "architecture": image.Architecture, "platforms": image.Platforms,
		"dockerfileDigest": prepared.DockerfileDigest,
	})
	return artifact
}

func composeServiceImageTag(tag, service string) string {
	service = strings.ToLower(service)
	var normalized strings.Builder
	for _, r := range service {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '.' || r == '-' {
			normalized.WriteRune(r)
		} else {
			normalized.WriteByte('-')
		}
	}
	suffix := strings.Trim(normalized.String(), ".-")
	if suffix == "" {
		suffix = "service"
	}
	return tag + "-" + suffix
}

func redactBuildEmitter(values map[string]string, emit func(BuildLog) error) func(BuildLog) error {
	secrets := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" {
			secrets = append(secrets, value)
		}
	}
	sort.Slice(secrets, func(i, j int) bool { return len(secrets[i]) > len(secrets[j]) })
	return func(line BuildLog) error {
		for _, secret := range secrets {
			line.Text = strings.ReplaceAll(line.Text, secret, "[REDACTED]")
		}
		return emit(line)
	}
}

func writeGeneratedDockerfile(root, content string) error {
	directory := filepath.Join(root, ".just-dashboard")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	path := filepath.Join(directory, "Dockerfile")
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, []byte(content), 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

func readContainedRegular(root, relative string, limit int64) ([]byte, error) {
	if !safeRelativePath(relative) {
		return nil, fmt.Errorf("path escapes the build context")
	}
	path := filepath.Join(root, filepath.Clean(relative))
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	realPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(realRoot, realPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return nil, fmt.Errorf("path escapes the build context")
	}
	info, err := os.Lstat(realPath)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > limit {
		return nil, fmt.Errorf("path is not a bounded regular file")
	}
	return os.ReadFile(realPath)
}

func validateCustomDockerfile(content []byte) error {
	text := string(content)
	if strings.Contains(strings.ToLower(text), "-----begin private key-----") || containsURLCredentials(text) {
		return fmt.Errorf("%w: custom Dockerfile contains credential material", ErrUnsupportedBuilder)
	}
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		instruction, rest, found := strings.Cut(line, " ")
		if !found {
			continue
		}
		switch strings.ToUpper(instruction) {
		case "ENV", "ARG", "RUN", "CMD", "ENTRYPOINT":
			if secretAssignmentRE.MatchString(" "+rest) || secretCommandFlagRE.MatchString(" "+rest) {
				return fmt.Errorf("%w: custom Dockerfile may place credential material in image layers or argv", ErrUnsupportedBuilder)
			}
		}
	}
	return nil
}

func regularExists(root, relative string) bool {
	info, err := os.Lstat(filepath.Join(root, filepath.Clean(relative)))
	return err == nil && info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0
}

func findGoMainPackages(root string, maxFiles int) ([]string, error) {
	seen := map[string]bool{}
	count := 0
	errStop := errors.New("Go source scan exceeded its bound")
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if entry.IsDir() && rel != "." {
			name := entry.Name()
			if name == ".git" || name == "vendor" || name == "node_modules" || name == ".just-dashboard" {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		count++
		if count > maxFiles {
			return errStop
		}
		content, err := readContainedRegular(root, rel, 1<<20)
		if err != nil {
			return err
		}
		if strings.Contains(string(content), "package main") {
			directory := filepath.ToSlash(filepath.Dir(rel))
			seen[directory] = true
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnsupportedBuilder, err)
	}
	result := make([]string, 0, len(seen))
	for directory := range seen {
		result = append(result, directory)
	}
	sort.Strings(result)
	return result, nil
}

func selectPythonLock(root string) (string, error) {
	for _, candidate := range []string{"uv.lock", "poetry.lock"} {
		if regularExists(root, candidate) {
			return candidate, nil
		}
	}
	requirements, err := readContainedRegular(root, "requirements.txt", 2<<20)
	if err != nil {
		return "", fmt.Errorf("%w: Python recipe requires uv.lock, poetry.lock, or pinned requirements.txt", ErrUnsupportedBuilder)
	}
	seen := false
	for _, raw := range strings.Split(string(requirements), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "--") {
			continue
		}
		seen = true
		if !strings.Contains(line, "==") && !strings.Contains(line, "@") {
			return "", fmt.Errorf("%w: requirements.txt entry %q is not pinned", ErrUnsupportedBuilder, line)
		}
	}
	if !seen {
		return "", fmt.Errorf("%w: requirements.txt has no pinned dependencies", ErrUnsupportedBuilder)
	}
	return "requirements.txt", nil
}

func digestText(value string) string {
	hash := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(hash[:])
}

func releaseImageTag(environmentID, runID int64) string {
	return "just-dashboard/deployment-" + strconv.FormatInt(environmentID, 10) + ":run-" + strconv.FormatInt(runID, 10)
}
