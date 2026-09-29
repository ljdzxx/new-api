package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"fmt"
	"html"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/groupmonitor"
	monitorconfig "github.com/QuantumNous/new-api/setting/group_monitor"
	"github.com/QuantumNous/new-api/setting/image_storage_setting"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
	htmlparser "golang.org/x/net/html"
)

type MonitorArtwork struct {
	PreviewVersion int    `json:"preview_version,omitempty"`
	PreviewURL     string `json:"preview_url,omitempty"`
	URLExpiresAt   int64  `json:"url_expires_at,omitempty"`
	ID             string `json:"id"`
	At             int64  `json:"at"`
	Model          string `json:"model"`
	HTMLKey        string `json:"html_key,omitempty"`
	HTMLURL        string `json:"html_url,omitempty"`
}

//go:embed group_monitor_preview.js
var monitorPreviewScript string

var monitorDocumentStart = regexp.MustCompile(`(?i)<!doctype\s+html|<html\b|<svg\b`)
var monitorRecordID = regexp.MustCompile(`^\d{13}-[0-9a-f-]{36}$`)

// Preview correctness never determines model success. Keep the original reply
// on disk and render a passive HTML copy inside the UI's sandboxed iframe.
func monitorHTML(answer string) (string, error) {
	content := strings.TrimSpace(answer)
	if at := monitorDocumentStart.FindStringIndex(content); at != nil {
		content = content[at[0]:]
		lower := strings.ToLower(content)
		for _, end := range []string{"</html>", "</svg>"} {
			if i := strings.LastIndex(lower, end); i >= 0 {
				content = content[:i+len(end)]
				break
			}
		}
	} else {
		content = "<pre>" + html.EscapeString(content) + "</pre>"
	}
	root, err := htmlparser.Parse(strings.NewReader(content))
	if err != nil {
		return "", err
	}
	var clean func(*htmlparser.Node)
	clean = func(n *htmlparser.Node) {
		for child := n.FirstChild; child != nil; {
			next := child.NextSibling
			switch strings.ToLower(child.Data) {
			case "script", "meta", "base", "link", "iframe", "frame", "frameset", "object", "embed", "applet":
				if child.Type == htmlparser.ElementNode {
					n.RemoveChild(child)
					child = next
					continue
				}
			}
			attrs := child.Attr[:0]
			for _, a := range child.Attr {
				name := strings.ToLower(a.Key)
				if strings.HasPrefix(name, "on") || name == "srcdoc" || name == "action" || name == "formaction" {
					continue
				}
				if name == "href" && !strings.HasPrefix(strings.TrimSpace(a.Val), "#") {
					continue
				}
				attrs = append(attrs, a)
			}
			child.Attr = attrs
			clean(child)
			child = next
		}
	}
	clean(root)
	var contents strings.Builder
	// Render head/body children under our own document and security policy.
	var render func(*htmlparser.Node) error
	render = func(n *htmlparser.Node) error {
		if n.Type == htmlparser.ElementNode && (n.Data == "head" || n.Data == "body") {
			for child := n.FirstChild; child != nil; child = child.NextSibling {
				if err := htmlparser.Render(&contents, child); err != nil {
					return err
				}
			}
			return nil
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			if err := render(child); err != nil {
				return err
			}
		}
		return nil
	}
	if err := render(root); err != nil {
		return "", err
	}
	hash := sha256.Sum256([]byte(monitorPreviewScript))
	policy := "default-src 'none'; script-src 'sha256-" + base64.StdEncoding.EncodeToString(hash[:]) + "'; style-src 'unsafe-inline'; img-src data:; font-src data:; base-uri 'none'; form-action 'none'; object-src 'none'"
	return `<!doctype html><html><head><meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="` + policy + `"><meta name="viewport" content="width=device-width,initial-scale=1"><style>html,body{margin:0;background:#fff}pre{white-space:pre-wrap;overflow-wrap:anywhere}</style></head><body>` + contents.String() + `<style>body>svg{display:block;width:100%!important;height:auto!important;max-height:none!important}</style><script>` + monitorPreviewScript + `</script></body></html>`, nil
}

func monitorR2Config(cfg monitorconfig.Config) image_storage_setting.ImageStorageSetting {
	common.OptionMapRWMutex.RLock()
	s := *image_storage_setting.GetImageStorageSetting()
	common.OptionMapRWMutex.RUnlock()
	s.R2ObjectPrefix, s.R2URLExpireHours = cfg.SVGPrefix, cfg.SVGURLHours
	return s
}

func monitorGroupHash(group string) string {
	hash := sha256.Sum256([]byte(group))
	return fmt.Sprintf("%x", hash[:16])
}

func monitorLocalDir(cfg monitorconfig.Config, group, id string) (string, error) {
	if !monitorRecordID.MatchString(id) {
		return "", fmt.Errorf("invalid SVG record ID")
	}
	root, err := filepath.Abs(cfg.SVGOutputDir)
	if err != nil {
		return "", err
	}
	return filepath.Join(root, monitorGroupHash(group), id), nil
}

// Write diagnostics locally before any R2 request. A failed model response is
// also archived, including partial text. Storage errors never change OK/Status.
func storeMonitorArtwork(ctx context.Context, cfg monitorconfig.Config, group string, result *MonitorResult) (resultErr error) {
	dir, err := monitorLocalDir(cfg, group, result.ID)
	if err != nil {
		return monitorError("local_output_failed", err)
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return monitorError("local_output_failed", err)
	}
	defer func() {
		if resultErr != nil {
			result.ArtifactError = monitorErrorCode(resultErr, "artwork_processing_failed")
			if err := writeMonitorLocalResult(dir, result); err != nil {
				common.SysError("group monitor local result: " + err.Error())
			}
		}
	}()
	files := []struct{ name, body, contentType string }{
		{"prompt.txt", result.Prompt, "text/plain; charset=utf-8"},
		{"response.txt", result.Answer, "text/plain; charset=utf-8"},
	}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(dir, f.name), []byte(f.body), 0600); err != nil {
			return monitorError("local_output_failed", err)
		}
	}
	if err := writeMonitorLocalResult(dir, result); err != nil {
		return monitorError("local_output_failed", err)
	}
	if result.OK {
		document, err := monitorHTML(result.Answer)
		if err != nil {
			return monitorError("html_preview_failed", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "artwork.html"), []byte(document), 0600); err != nil {
			return monitorError("local_output_failed", err)
		}
		files = append(files, struct{ name, body, contentType string }{"artwork.html", document, "text/html; charset=utf-8"})
	}
	s := monitorR2Config(cfg)
	client, err := newR2S3Client(&s)
	if err != nil {
		return monitorError("r2_config_failed", err)
	}
	prefix := strings.Trim(cfg.SVGPrefix, "/ ") + "/" + monitorGroupHash(group) + "/"
	objectKey := func(name string) string { return prefix + result.ID + "/" + name }
	for _, f := range files {
		_, err := client.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(s.R2Bucket), Key: aws.String(objectKey(f.name)), Body: strings.NewReader(f.body), ContentType: aws.String(f.contentType), CacheControl: aws.String("private, max-age=3600")})
		if err != nil {
			return monitorError("r2_upload_failed", fmt.Errorf("upload %s: %w", f.name, err))
		}
	}
	raw, err := common.Marshal(result)
	if err != nil {
		return monitorError("artwork_processing_failed", err)
	}
	if _, err := client.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(s.R2Bucket), Key: aws.String(objectKey("result.json")), Body: bytes.NewReader(raw), ContentType: aws.String("application/json")}); err != nil {
		return monitorError("r2_upload_failed", err)
	}
	if result.OK {
		art := MonitorArtwork{ID: result.ID, At: result.At, Model: result.Model, HTMLKey: objectKey("artwork.html"), PreviewVersion: 2}
		raw, err := common.Marshal(art)
		if err != nil {
			return err
		}
		key := groupmonitor.Key(group, "", "artworks")
		_, err = common.RDB.TxPipelined(ctx, func(p redis.Pipeliner) error {
			p.LPush(ctx, key, string(raw))
			p.LTrim(ctx, key, 0, int64(cfg.SVGKeep-1))
			p.Expire(ctx, key, monitorRecordTTL)
			return nil
		})
		if err != nil {
			return monitorError("artwork_index_failed", err)
		}
	}
	if err := pruneMonitorGroup(ctx, cfg, group, client, s.R2Bucket, prefix); err != nil {
		return monitorError("r2_retention_failed", err)
	}
	return nil
}

func writeMonitorLocalResult(dir string, result *MonitorResult) error {
	raw, err := common.Marshal(result)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "result.json"), raw, 0600)
}

// Retain whole runs, including diagnostic files and old HTML/PNG pairs.
func pruneMonitorObjects(ctx context.Context, client *s3.Client, bucket, prefix string, keep int) (map[string]bool, error) {
	pages := s3.NewListObjectsV2Paginator(client, &s3.ListObjectsV2Input{Bucket: aws.String(bucket), Prefix: aws.String(prefix)})
	runs := map[string][]string{}
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, o := range page.Contents {
			key := aws.ToString(o.Key)
			if !strings.HasPrefix(key, prefix) {
				continue
			}
			rest := strings.TrimPrefix(key, prefix)
			id, _, nested := strings.Cut(rest, "/")
			if !nested {
				id = strings.TrimSuffix(rest, filepath.Ext(rest))
			}
			if id == "" {
				continue
			}
			runs[id] = append(runs[id], key)
		}
	}
	ids := make([]string, 0, len(runs))
	for id := range runs {
		ids = append(ids, id)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(ids)))
	retained := map[string]bool{}
	for i, id := range ids {
		if i < keep {
			retained[id] = true
			continue
		}
		for _, key := range runs[id] {
			if _, err := client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)}); err != nil {
				return nil, err
			}
		}
	}
	return retained, nil
}

func pruneMonitorGroup(ctx context.Context, cfg monitorconfig.Config, group string, client *s3.Client, bucket, prefix string) error {
	retained, err := pruneMonitorObjects(ctx, client, bucket, prefix, cfg.SVGKeep)
	if err != nil {
		return err
	}
	key := groupmonitor.Key(group, "", "artworks")
	values, err := common.RDB.LRange(ctx, key, 0, -1).Result()
	if err != nil {
		return err
	}
	_, err = common.RDB.TxPipelined(ctx, func(p redis.Pipeliner) error {
		for _, raw := range values {
			var art MonitorArtwork
			if common.UnmarshalJsonStr(raw, &art) != nil || !retained[art.ID] {
				p.LRem(ctx, key, 0, raw)
			}
		}
		return nil
	})
	return err
}

func GetMonitorArtworks(ctx context.Context, cfg monitorconfig.Config, group string) ([]MonitorArtwork, error) {
	values, err := common.RDB.LRange(ctx, groupmonitor.Key(group, "", "artworks"), 0, int64(cfg.SVGKeep-1)).Result()
	if err != nil {
		return nil, err
	}
	arts := []MonitorArtwork{}
	if len(values) == 0 {
		return arts, nil
	}
	s := monitorR2Config(cfg)
	client, err := newR2S3Client(&s)
	if err != nil {
		return nil, err
	}
	presign := s3.NewPresignClient(client)
	for _, raw := range values {
		var a MonitorArtwork
		if common.UnmarshalJsonStr(raw, &a) != nil {
			continue
		}
		if cfg.SVGPublicBaseURL != "" && a.PreviewVersion >= 2 {
			base, err := url.Parse(cfg.SVGPublicBaseURL)
			if err != nil {
				return nil, err
			}
			base.Path = strings.TrimRight(base.Path, "/") + "/" + a.HTMLKey
			base.RawPath = ""
			a.HTMLURL = base.String()
		} else {
			link, err := presign.PresignGetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.R2Bucket), Key: aws.String(a.HTMLKey)}, func(o *s3.PresignOptions) { o.Expires = time.Duration(cfg.SVGURLHours) * time.Hour })
			if err != nil {
				return nil, err
			}
			a.HTMLURL = link.URL
			a.URLExpiresAt = time.Now().Add(time.Duration(cfg.SVGURLHours) * time.Hour).UnixMilli()
		}
		a.HTMLKey = ""
		if a.PreviewVersion < 2 {
			a.PreviewURL = "/api/monitor/preview?group=" + url.QueryEscape(group) + "&id=" + url.QueryEscape(a.ID)
		}

		arts = append(arts, a)
	}
	return arts, nil
}

func maintainMonitorArtworks(cfg monitorconfig.Config) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	for group := range cfg.Groups {
		for _, kind := range []string{"logic", "svg"} {
			if err := trimMonitorHistory(ctx, group, kind, cfg); err != nil {
				common.SysError("group monitor history cleanup failed: " + err.Error())
			}
		}
	}
	s := monitorR2Config(cfg)
	if s.R2Bucket == "" || s.R2AccessKeyID == "" {
		return
	}
	client, err := newR2S3Client(&s)
	if err != nil {
		return
	}
	for group := range cfg.Groups {
		lease, owner := groupmonitor.Key(group, "", "svg:lock"), uuid.NewString()
		ok, err := common.RDB.SetNX(ctx, lease, owner, 3*time.Minute).Result()
		if err != nil {
			return
		}
		if !ok {
			continue
		}
		prefix := strings.Trim(cfg.SVGPrefix, "/ ") + "/" + monitorGroupHash(group) + "/"
		err = pruneMonitorGroup(ctx, cfg, group, client, s.R2Bucket, prefix)
		if err == nil {
			err = common.RDB.LTrim(ctx, groupmonitor.Key(group, "", "artworks"), 0, int64(cfg.SVGKeep-1)).Err()
		}
		releaseCtx, done := context.WithTimeout(context.Background(), time.Second)
		_ = releaseMonitorLease.Run(releaseCtx, common.RDB, []string{lease}, owner).Err()
		done()
		if err != nil {
			common.SysError("group monitor retention cleanup failed: " + err.Error())
		}
	}
}

// Older objects have no sizing script. Render them through the same current
// sanitizer without changing archived files or writing back to R2.
func GetMonitorLegacyPreview(ctx context.Context, cfg monitorconfig.Config, group, id string) (string, error) {
	values, err := common.RDB.LRange(ctx, groupmonitor.Key(group, "", "artworks"), 0, int64(cfg.SVGKeep-1)).Result()
	if err != nil {
		return "", err
	}
	var key string
	for _, raw := range values {
		var art MonitorArtwork
		if common.UnmarshalJsonStr(raw, &art) == nil && art.ID == id {
			key = art.HTMLKey
			break
		}
	}
	if key == "" {
		return "", redis.Nil
	}
	s := monitorR2Config(cfg)
	client, err := newR2S3Client(&s)
	if err != nil {
		return "", err
	}
	object, err := client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.R2Bucket), Key: aws.String(key)})
	if err != nil {
		return "", err
	}
	defer object.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(object.Body, (2<<20)+1))
	if err != nil {
		return "", err
	}
	if len(raw) > 2<<20 {
		return "", fmt.Errorf("legacy HTML too large")
	}
	return monitorHTML(string(raw))
}
