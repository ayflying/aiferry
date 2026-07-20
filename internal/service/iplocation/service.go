package iplocation

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/frame/g"
	ip2service "github.com/lionsoul2014/ip2region/binding/golang/service"
	"github.com/lionsoul2014/ip2region/binding/golang/xdb"
)

const (
	maxDatabaseSize         = 64 << 20
	databaseDownloadTimeout = 2 * time.Minute
	v4DatabaseName          = "ip2region_v4.xdb"
	v6DatabaseName          = "ip2region_v6.xdb"
	v4DatabaseURL           = "https://raw.githubusercontent.com/lionsoul2014/ip2region/master/data/ip2region_v4.xdb"
	v6DatabaseURL           = "https://raw.githubusercontent.com/lionsoul2014/ip2region/master/data/ip2region_v6.xdb"
)

var defaultDatabases = []database{
	{name: v4DatabaseName, url: v4DatabaseURL},
	{name: v6DatabaseName, url: v6DatabaseURL},
}

type database struct {
	name string
	url  string
}

type Service struct {
	searcher atomic.Pointer[ip2service.Ip2Region]
}

func New(ctx context.Context, client *http.Client, dataDir string) *Service {
	return newService(ctx, client, dataDir, defaultDatabases, databaseDownloadTimeout)
}

func newService(ctx context.Context, client *http.Client, dataDir string, databases []database, downloadTimeout time.Duration) *Service {
	if client == nil {
		client = http.DefaultClient
	}
	service := &Service{}
	paths, complete := localDatabasePaths(dataDir, databases)
	if complete {
		service.activate(ctx, paths)
		return service
	}

	g.Log().Info(ctx, "IP location databases unavailable locally; downloading in background")
	go service.downloadAndActivate(ctx, client, dataDir, databases, downloadTimeout)
	return service
}

func localDatabasePaths(dataDir string, databases []database) ([]string, bool) {
	paths := make([]string, len(databases))
	for index, item := range databases {
		path := filepath.Join(dataDir, item.name)
		if err := xdb.VerifyFromFile(path); err != nil {
			return paths, false
		}
		paths[index] = path
	}
	return paths, true
}

func (s *Service) downloadAndActivate(ctx context.Context, client *http.Client, dataDir string, databases []database, downloadTimeout time.Duration) {
	downloadCtx, cancel := context.WithTimeout(ctx, downloadTimeout)
	defer cancel()

	paths := make([]string, len(databases))
	for index, item := range databases {
		path, err := ensureDatabase(downloadCtx, client, dataDir, item)
		if err != nil {
			g.Log().Warningf(ctx, "IP location database %s unavailable: %v", item.name, err)
			return
		}
		paths[index] = path
	}
	s.activate(ctx, paths)
}

func (s *Service) activate(ctx context.Context, paths []string) {
	if len(paths) != 2 {
		g.Log().Warningf(ctx, "IP location lookup disabled: expected 2 databases, got %d", len(paths))
		return
	}
	searcher, err := ip2service.NewIp2RegionWithPath(paths[0], paths[1])
	if err != nil {
		g.Log().Warningf(ctx, "IP location lookup disabled: %v", err)
		return
	}
	s.searcher.Store(searcher)
	g.Log().Info(ctx, "IP location lookup enabled")
}

func (s *Service) Lookup(value string) string {
	ip := NormalizeIP(value)
	if ip == "" {
		return ""
	}
	address, err := netip.ParseAddr(ip)
	if err != nil || address.IsUnspecified() {
		return ""
	}
	if address.IsLoopback() {
		return "本机地址"
	}
	if address.IsPrivate() || address.IsLinkLocalUnicast() {
		return "内网地址"
	}
	searcher := s.searcher.Load()
	if searcher == nil {
		return ""
	}
	region, err := searcher.Search(ip)
	if err != nil {
		return ""
	}
	return formatRegion(region)
}

func NormalizeIP(value string) string {
	value = strings.TrimSpace(value)
	address, err := netip.ParseAddr(value)
	if err != nil {
		host, _, splitErr := net.SplitHostPort(value)
		if splitErr != nil {
			return ""
		}
		address, err = netip.ParseAddr(host)
	}
	if err != nil {
		return ""
	}
	return address.Unmap().String()
}

func ensureDatabase(ctx context.Context, client *http.Client, dataDir string, item database) (string, error) {
	path := filepath.Join(dataDir, item.name)
	if err := xdb.VerifyFromFile(path); err == nil {
		return path, nil
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return "", gerror.Wrap(err, "create IP location data directory")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, item.url, nil)
	if err != nil {
		return "", gerror.Wrap(err, "create IP location database download request")
	}
	response, err := client.Do(request)
	if err != nil {
		return "", gerror.Wrap(err, "download IP location database")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", gerror.Newf("download IP location database returned %s", response.Status)
	}
	temporary, err := os.CreateTemp(dataDir, "."+item.name+"-*")
	if err != nil {
		return "", gerror.Wrap(err, "create temporary IP location database")
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	written, copyErr := io.Copy(temporary, io.LimitReader(response.Body, maxDatabaseSize+1))
	if closeErr := temporary.Close(); copyErr != nil {
		return "", gerror.Wrap(copyErr, "save IP location database")
	} else if closeErr != nil {
		return "", gerror.Wrap(closeErr, "close IP location database")
	}
	if written > maxDatabaseSize {
		return "", gerror.New("IP location database exceeds allowed size")
	}
	if err = xdb.VerifyFromFile(temporaryPath); err != nil {
		return "", gerror.Wrap(err, "verify downloaded IP location database")
	}
	if err = os.Rename(temporaryPath, path); err != nil {
		return "", gerror.Wrap(err, "activate downloaded IP location database")
	}
	return path, nil
}

func formatRegion(region string) string {
	values := strings.Split(region, "|")
	if len(values) > 1 && len(values[len(values)-1]) == 2 {
		values = values[:len(values)-1]
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || value == "0" || (len(result) > 0 && result[len(result)-1] == value) {
			continue
		}
		result = append(result, value)
	}
	return strings.Join(result, " / ")
}
