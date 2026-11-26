package channel

import (
	"context"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	adminapi "github.com/yunloli/aiferry/api/admin"
)

// https://docs.cloud.google.com/resource-manager/reference/rest/v3/projects/search
const (
	geminiProjectSearchURL       = "https://cloudresourcemanager.googleapis.com/v3/projects:search"
	geminiProjectReadOnlyScope   = "https://www.googleapis.com/auth/cloudplatformprojects.readonly"
	geminiProjectSearchTimeout   = 6 * time.Second
	geminiProjectSearchPageSize  = 25
	geminiProjectSearchMaxPages  = 4
	geminiProjectSearchMaxItems  = geminiProjectSearchPageSize * geminiProjectSearchMaxPages
	geminiProjectSearchMaxCursor = 2048
)

const (
	geminiProjectPickMessage    = "请选择 Google Cloud 项目，或手动输入项目 ID。"
	geminiProjectNoScopeMessage = "未授予读取项目列表的权限，请手动输入 Google Cloud 项目 ID。"
	geminiProjectUnavailableMsg = "无法读取 Google Cloud 项目列表，请手动输入项目 ID。"
	geminiProjectEmptyMessage   = "未找到可列出的有效 Google Cloud 项目，请手动输入项目 ID。"
	geminiProjectPartialMessage = "仅显示部分 Google Cloud 项目，未列出的项目可手动输入 ID。"
)

func geminiProjectChoices(ctx context.Context, client *http.Client, tokens geminiOAuthTokens) ([]adminapi.ChannelLoginProject, string) {
	// 按 OAuth 2.0，scope 省略表示与请求相同。若 Google 明确没有授予列表权限，
	// 不尝试用已有的 cloud-platform scope 代替未批准的项目读取权限。
	if tokens.Scope != "" {
		granted := false
		for _, scope := range strings.Fields(tokens.Scope) {
			granted = granted || scope == geminiProjectReadOnlyScope
		}
		if !granted {
			return nil, geminiProjectNoScopeMessage
		}
	}
	ctx, cancel := context.WithTimeout(ctx, geminiProjectSearchTimeout)
	defer cancel()
	query := url.Values{"query": {"state:ACTIVE"}, "pageSize": {strconv.Itoa(geminiProjectSearchPageSize)}, "fields": {"nextPageToken,projects(projectId,displayName,state)"}}
	seen := make(map[string]bool)
	cursors := make(map[string]bool)
	var projects []adminapi.ChannelLoginProject
	message := geminiProjectPartialMessage
	for range geminiProjectSearchMaxPages {
		var reply struct {
			Projects []struct {
				ProjectID   string `json:"projectId"`
				DisplayName string `json:"displayName"`
				State       string `json:"state"`
			} `json:"projects"`
			NextPageToken string `json:"nextPageToken"`
		}
		if err := geminiOAuthRequest(ctx, client, geminiProjectSearchURL+"?"+query.Encode(), nil, tokens.AccessToken, &reply); err != nil {
			if len(projects) == 0 {
				return nil, geminiProjectUnavailableMsg
			}
			break
		}
		for _, p := range reply.Projects {
			if p.State != "ACTIVE" || !geminiProjectIDPattern.MatchString(p.ProjectID) || seen[p.ProjectID] {
				continue
			}
			if len(projects) == geminiProjectSearchMaxItems {
				break
			}
			seen[p.ProjectID] = true
			projects = append(projects, adminapi.ChannelLoginProject{ProjectID: p.ProjectID, DisplayName: geminiProjectDisplayName(p.DisplayName, p.ProjectID)})
		}
		next := reply.NextPageToken
		if next == "" && len(projects) < geminiProjectSearchMaxItems {
			message = geminiProjectPickMessage
			break
		}
		if len(projects) == geminiProjectSearchMaxItems || len(next) > geminiProjectSearchMaxCursor || cursors[next] {
			break
		}
		cursors[next] = true
		query.Set("pageToken", next)
	}
	if len(projects) == 0 {
		return nil, geminiProjectEmptyMessage
	}
	sort.Slice(projects, func(i, j int) bool { return projects[i].ProjectID < projects[j].ProjectID })
	return projects, message
}

func geminiProjectDisplayName(name, id string) string {
	name = strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf) {
			return -1
		}
		return r
	}, name))
	if name == "" {
		return id
	}
	if runes := []rune(name); len(runes) > 128 {
		return string(runes[:128])
	}
	return name
}
