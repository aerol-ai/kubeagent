package upgrade

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const defaultRegistry = "registry-1.docker.io"

type imageReference struct {
	Registry   string
	Repository string
}

type registryTagsResponse struct {
	Tags []string `json:"tags"`
}

type registryTokenResponse struct {
	Token       string `json:"token"`
	AccessToken string `json:"access_token"`
}

// LatestRemoteVersion returns the newest semver tag available for an image repository.
func LatestRemoteVersion(ctx context.Context, imageRepo string) (string, error) {
	ref, err := parseImageReference(imageRepo)
	if err != nil {
		return "", err
	}

	httpClient := &http.Client{Timeout: autoUpgradeCheckTimeout}
	tagsURL := fmt.Sprintf("https://%s/v2/%s/tags/list?n=1000", ref.Registry, ref.Repository)

	resp, err := doRegistryRequest(ctx, httpClient, tagsURL, "")
	if err != nil {
		return "", err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		token, tokenErr := fetchRegistryToken(ctx, httpClient, resp.Header.Get("Www-Authenticate"))
		resp.Body.Close()
		if tokenErr != nil {
			return "", tokenErr
		}

		resp, err = doRegistryRequest(ctx, httpClient, tagsURL, token)
		if err != nil {
			return "", err
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("registry returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var payload registryTagsResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}

	latest := ""
	for _, tag := range payload.Tags {
		if NormalizeVersion(tag) == "" {
			continue
		}
		if latest == "" || IsNewerVersion(tag, latest) {
			latest = tag
		}
	}
	if latest == "" {
		return "", fmt.Errorf("no semver tags found for %s", imageRepo)
	}

	return latest, nil
}

func doRegistryRequest(ctx context.Context, httpClient *http.Client, endpoint, token string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "kubeagent-auto-upgrade")
	return httpClient.Do(req)
}

func fetchRegistryToken(ctx context.Context, httpClient *http.Client, challenge string) (string, error) {
	params, err := parseAuthChallenge(challenge)
	if err != nil {
		return "", err
	}

	tokenURL, err := url.Parse(params["realm"])
	if err != nil {
		return "", err
	}
	query := tokenURL.Query()
	if service := params["service"]; service != "" {
		query.Set("service", service)
	}
	if scope := params["scope"]; scope != "" {
		query.Set("scope", scope)
	}
	tokenURL.RawQuery = query.Encode()

	resp, err := doRegistryRequest(ctx, httpClient, tokenURL.String(), "")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("token endpoint returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var payload registryTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}
	if payload.Token != "" {
		return payload.Token, nil
	}
	if payload.AccessToken != "" {
		return payload.AccessToken, nil
	}
	return "", fmt.Errorf("token response did not include an access token")
}

func parseImageReference(imageRepo string) (imageReference, error) {
	trimmed := strings.TrimSpace(imageRepo)
	if trimmed == "" {
		return imageReference{}, fmt.Errorf("image repository is required")
	}
	if at := strings.Index(trimmed, "@"); at >= 0 {
		trimmed = trimmed[:at]
	}

	parts := strings.Split(trimmed, "/")
	if len(parts) == 0 {
		return imageReference{}, fmt.Errorf("invalid image repository %q", imageRepo)
	}

	registry := defaultRegistry
	repository := trimmed
	if isRegistryHost(parts[0]) {
		registry = parts[0]
		repository = strings.Join(parts[1:], "/")
	} else if len(parts) == 1 {
		repository = "library/" + parts[0]
	}

	if repository == "" {
		return imageReference{}, fmt.Errorf("invalid image repository %q", imageRepo)
	}

	lastSlash := strings.LastIndex(repository, "/")
	lastColon := strings.LastIndex(repository, ":")
	if lastColon > lastSlash {
		repository = repository[:lastColon]
	}

	return imageReference{Registry: registry, Repository: repository}, nil
}

func parseAuthChallenge(challenge string) (map[string]string, error) {
	trimmed := strings.TrimSpace(challenge)
	if trimmed == "" {
		return nil, fmt.Errorf("registry did not provide an authentication challenge")
	}
	parts := strings.SplitN(trimmed, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return nil, fmt.Errorf("unsupported auth challenge %q", challenge)
	}

	params := map[string]string{}
	for _, field := range strings.Split(parts[1], ",") {
		keyValue := strings.SplitN(strings.TrimSpace(field), "=", 2)
		if len(keyValue) != 2 {
			continue
		}
		params[keyValue[0]] = strings.Trim(keyValue[1], `"`)
	}
	if params["realm"] == "" {
		return nil, fmt.Errorf("registry auth challenge is missing a realm")
	}
	return params, nil
}

func isRegistryHost(part string) bool {
	return strings.Contains(part, ".") || strings.Contains(part, ":") || part == "localhost"
}
