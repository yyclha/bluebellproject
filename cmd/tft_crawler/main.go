package main

import (
	"bluebell/internal/dao/redis"
	"bluebell/internal/setting"
	"bluebell/pkg/snowflake"
	"crypto/md5"
	"database/sql"
	"encoding/hex"
	"flag"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"
)

const (
	patchIndexURL = "https://teamfighttactics.leagueoflegends.com/en-us/news/tags/patch-notes/"
	fallbackPatch = "https://teamfighttactics.leagueoflegends.com/en-us/news/game-updates/teamfight-tactics-patch-17-1/"
	fallbackSet   = "https://teamfighttactics.leagueoflegends.com/en-us/news/game-updates/tft-set-17-space-gods-overview/"

	crawlerAuthorName = "tft_crawler"
	tftCommunityName  = "云顶之弈"
	tftIntro          = "阵容搭配、运营节奏、版本答案与上分复盘"
)

var (
	hrefPatchRe = regexp.MustCompile(`href="([^"]*teamfight-tactics-patch-[^"]*/)"`)
	hrefRe      = regexp.MustCompile(`href="([^"]+)"`)
	titleRe     = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	metaDescRe  = regexp.MustCompile(`(?is)<meta[^>]+name=["']description["'][^>]+content=["']([^"']*)["']`)
	ogTitleRe   = regexp.MustCompile(`(?is)<meta[^>]+property=["']og:title["'][^>]+content=["']([^"']*)["']`)
	blockRe     = regexp.MustCompile(`(?is)<(?:h1|h2|h3|h4|p|li|blockquote)[^>]*>(.*?)</(?:h1|h2|h3|h4|p|li|blockquote)>`)
	tagRe       = regexp.MustCompile(`(?is)<[^>]+>`)
	spaceRe     = regexp.MustCompile(`\s+`)
)

type crawlSource struct {
	Title       string
	URL         string
	Description string
	Points      []string
	Links       []string
}

type seedArticle struct {
	Title   string
	Content string
}

// main 程序入口，抓取云顶之弈官方公开资料并生成站内攻略帖。
func main() {
	configPath := flag.String("config", "./conf/config.yaml", "config file path")
	dryRun := flag.Bool("dry-run", false, "print articles without writing database")
	maxPosts := flag.Int("max-posts", 3, "max posts to write")
	flag.Parse()

	if *maxPosts <= 0 {
		fmt.Println("max-posts must be greater than 0")
		os.Exit(1)
	}

	if err := setting.Init(*configPath); err != nil {
		fmt.Printf("load config failed, err:%v\n", err)
		os.Exit(1)
	}
	if err := snowflake.Init(setting.Conf.StartTime, setting.Conf.MachineID); err != nil {
		fmt.Printf("init snowflake failed, err:%v\n", err)
		os.Exit(1)
	}

	db, err := openDB(setting.Conf.MySQLConfig)
	if err != nil {
		fmt.Printf("connect mysql failed, err:%v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	if !*dryRun {
		if err := redis.Init(setting.Conf.RedisConfig); err != nil {
			fmt.Printf("connect redis failed, err:%v\n", err)
			os.Exit(1)
		}
		defer redis.Close()
	}

	sources, err := crawlTFTSources()
	if err != nil {
		fmt.Printf("crawl tft sources failed, err:%v\n", err)
		os.Exit(1)
	}
	articles := buildSeedArticles(sources)
	if len(articles) > *maxPosts {
		articles = articles[:*maxPosts]
	}

	if *dryRun {
		for _, article := range articles {
			fmt.Printf("\n===== %s =====\n%s\n", article.Title, article.Content)
		}
		return
	}

	communityID, err := ensureCommunity(db)
	if err != nil {
		fmt.Printf("ensure tft community failed, err:%v\n", err)
		os.Exit(1)
	}
	authorID, err := ensureAuthor(db)
	if err != nil {
		fmt.Printf("ensure crawler author failed, err:%v\n", err)
		os.Exit(1)
	}

	inserted := 0
	skipped := 0
	for _, article := range articles {
		ok, err := seedPost(db, article, authorID, communityID)
		if err != nil {
			fmt.Printf("seed post failed for %q, err:%v\n", article.Title, err)
			os.Exit(1)
		}
		if ok {
			inserted++
		} else {
			skipped++
		}
	}

	fmt.Printf("tft crawler complete, inserted=%d skipped=%d community_id=%d\n", inserted, skipped, communityID)
}

func openDB(cfg *setting.MySQLConfig) (*sqlx.DB, error) {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?parseTime=true&loc=Local&charset=utf8mb4",
		cfg.User,
		cfg.Password,
		cfg.Host,
		cfg.Port,
		cfg.DB,
	)
	db, err := sqlx.Connect("mysql", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)
	return db, nil
}

func crawlTFTSources() ([]crawlSource, error) {
	client := &http.Client{Timeout: 20 * time.Second}

	indexHTML, err := fetchText(client, patchIndexURL)
	if err != nil {
		return nil, err
	}
	patchURL := discoverLatestPatchURL(indexHTML)
	if patchURL == "" {
		patchURL = fallbackPatch
	}

	patchHTML, err := fetchText(client, patchURL)
	if err != nil {
		return nil, err
	}
	setURL := discoverSetOverviewURL(patchHTML)
	if setURL == "" {
		setURL = fallbackSet
	}

	setHTML, err := fetchText(client, setURL)
	if err != nil {
		return nil, err
	}

	return []crawlSource{
		parseSource(patchURL, patchHTML),
		parseSource(setURL, setHTML),
	}, nil
}

func fetchText(client *http.Client, rawURL string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "bluebell-tft-crawler/1.0 (+https://github.com/yyclha/bluebellproject)")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("GET %s returned %s", rawURL, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func discoverLatestPatchURL(indexHTML string) string {
	matches := hrefPatchRe.FindAllStringSubmatch(indexHTML, -1)
	seen := make(map[string]bool)
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		raw := normalizeRiotURL(match[1])
		if raw == "" || seen[raw] {
			continue
		}
		seen[raw] = true
		return raw
	}
	return ""
}

func discoverSetOverviewURL(patchHTML string) string {
	matches := hrefRe.FindAllStringSubmatch(patchHTML, -1)
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		link := html.UnescapeString(match[1])
		lower := strings.ToLower(link)
		if strings.Contains(lower, "space-gods-overview") || strings.Contains(lower, "set-17") {
			return normalizeRiotURL(link)
		}
	}
	return ""
}

func normalizeRiotURL(raw string) string {
	raw = html.UnescapeString(strings.TrimSpace(raw))
	if raw == "" || strings.HasPrefix(raw, "#") {
		return ""
	}
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		return raw
	}
	base, _ := url.Parse("https://teamfighttactics.leagueoflegends.com")
	ref, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return base.ResolveReference(ref).String()
}

func parseSource(rawURL, pageHTML string) crawlSource {
	source := crawlSource{
		Title:       extractTitle(pageHTML),
		Description: extractDescription(pageHTML),
		URL:         rawURL,
		Points:      extractPoints(pageHTML, 10),
		Links:       extractExternalGuideLinks(pageHTML),
	}
	if source.Title == "" {
		source.Title = rawURL
	}
	return source
}

func extractTitle(pageHTML string) string {
	for _, re := range []*regexp.Regexp{ogTitleRe, titleRe} {
		match := re.FindStringSubmatch(pageHTML)
		if len(match) >= 2 {
			return cleanText(match[1])
		}
	}
	return ""
}

func extractDescription(pageHTML string) string {
	match := metaDescRe.FindStringSubmatch(pageHTML)
	if len(match) < 2 {
		return ""
	}
	return cleanText(match[1])
}

func extractPoints(pageHTML string, limit int) []string {
	keywords := []string{
		"space gods",
		"set 17",
		"realm of the gods",
		"god",
		"boon",
		"ranked",
		"encounter",
		"augment",
		"item",
		"champion",
		"carousel",
	}

	matches := blockRe.FindAllStringSubmatch(pageHTML, -1)
	points := make([]string, 0, limit)
	seen := make(map[string]bool)
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		text := cleanText(match[1])
		if len([]rune(text)) < 18 || len([]rune(text)) > 260 {
			continue
		}
		lower := strings.ToLower(text)
		if !containsAny(lower, keywords) {
			continue
		}
		if seen[text] {
			continue
		}
		seen[text] = true
		points = append(points, text)
		if len(points) >= limit {
			break
		}
	}
	return points
}

func extractExternalGuideLinks(pageHTML string) []string {
	matches := hrefRe.FindAllStringSubmatch(pageHTML, -1)
	hosts := []string{"metatft.com", "mobalytics.gg", "tacter.com"}
	links := make([]string, 0, 3)
	seen := make(map[string]bool)
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		link := html.UnescapeString(match[1])
		lower := strings.ToLower(link)
		if !containsAny(lower, hosts) || seen[link] {
			continue
		}
		seen[link] = true
		links = append(links, link)
		if len(links) >= 3 {
			break
		}
	}
	return links
}

func buildSeedArticles(sources []crawlSource) []seedArticle {
	patch := firstSourceContaining(sources, "patch")
	overview := firstSourceContaining(sources, "overview")
	if patch.URL == "" && len(sources) > 0 {
		patch = sources[0]
	}
	if overview.URL == "" && len(sources) > 1 {
		overview = sources[1]
	}

	seasonName := detectSeasonName(sources)
	now := time.Now().Format("2006-01-02")

	quickStart := seedArticle{
		Title: fmt.Sprintf("云顶之弈 Set 17 %s 快速上手攻略", seasonName),
		Content: joinSections(
			fmt.Sprintf("采集时间：%s", now),
			fmt.Sprintf("赛季主题：Set 17 %s", seasonName),
			"这篇帖子由爬虫根据 Riot 官方公开页面整理，适合作为新赛季开荒入口。",
			"核心机制：",
			bulletList(mergePoints(overview.Points, patch.Points, 6)),
			"上分建议：",
			bulletList([]string{
				"开局先观察 Realm of the Gods / 神明赐福给到的组件和方向，再决定连胜、连败或经济节奏。",
				"前两阶段优先做通用强装备，等核心弈子和羁绊成型后再补专属装备。",
				"新赛季初阵容波动会比较大，建议先收藏 2-3 套低费过渡线，再根据商店来转高费主 C。",
				"如果版本刚更新，优先参考官方改动和数据站趋势，不要只照搬单一阵容。",
			}),
			sourceSection(patch, overview),
		),
	}

	patchGuide := seedArticle{
		Title: "云顶之弈 17.1 版本更新重点与开荒提醒",
		Content: joinSections(
			fmt.Sprintf("采集时间：%s", now),
			"版本定位：17.1 是 Set 17 Space Gods 的上线版本，适合先了解机制、排位重置和环境变化。",
			"官方页面摘要：",
			bulletList(nonEmptySlice([]string{patch.Description})),
			"值得优先关注：",
			bulletList(mergePoints(patch.Points, nil, 8)),
			"开荒提醒：",
			bulletList([]string{
				"先熟悉新赛季的基础机制，再追求特定阵容排名。",
				"排位初期波动大，保前四比强行追三星更稳定。",
				"遇到高强度热补丁时，优先更新过渡阵容和装备优先级。",
			}),
			sourceSection(patch),
		),
	}

	links := collectGuideLinks(sources)
	sourceGuide := seedArticle{
		Title: "云顶之弈最新赛季资料与阵容数据入口",
		Content: joinSections(
			fmt.Sprintf("采集时间：%s", now),
			"这个帖子整理最新赛季的官方资料入口和阵容数据入口，方便后续手动补充更细的阵容攻略。",
			"官方资料：",
			bulletList([]string{
				fmt.Sprintf("%s：%s", overview.Title, overview.URL),
				fmt.Sprintf("%s：%s", patch.Title, patch.URL),
			}),
			"阵容数据入口：",
			bulletList(links),
			"使用建议：",
			bulletList([]string{
				"官方资料看机制、改动和赛季规则。",
				"数据站看登场率、前四率、吃鸡率和装备搭配。",
				"站内帖子可以继续补充实战复盘，比如运营节奏、站位和变阵条件。",
			}),
		),
	}

	return []seedArticle{quickStart, patchGuide, sourceGuide}
}

func ensureCommunity(db *sqlx.DB) (int64, error) {
	var id int64
	err := db.Get(&id, `select community_id from community where community_name = ?`, tftCommunityName)
	if err == nil {
		_, updateErr := db.Exec(`update community set introduction = ? where community_name = ?`, tftIntro, tftCommunityName)
		return id, updateErr
	}
	if err != sql.ErrNoRows {
		return 0, err
	}

	for _, alias := range []string{"云顶之奕", "TFT", "金铲铲"} {
		err = db.Get(&id, `select community_id from community where community_name = ?`, alias)
		if err == nil {
			_, updateErr := db.Exec(`update community set community_name = ?, introduction = ? where community_id = ?`, tftCommunityName, tftIntro, id)
			return id, updateErr
		}
		if err != sql.ErrNoRows {
			return 0, err
		}
	}

	err = db.Get(&id, `select coalesce(max(community_id), 0) + 1 from community`)
	if err != nil {
		return 0, err
	}
	_, err = db.Exec(`insert into community(community_id, community_name, introduction) values (?, ?, ?)`, id, tftCommunityName, tftIntro)
	return id, err
}

func ensureAuthor(db *sqlx.DB) (int64, error) {
	var id int64
	err := db.Get(&id, `select user_id from user where username = ?`, crawlerAuthorName)
	if err == nil {
		return id, nil
	}
	if err != sql.ErrNoRows {
		return 0, err
	}

	id = snowflake.GenID()
	_, err = db.Exec(`insert into user(user_id, username, password) values (?, ?, ?)`, id, crawlerAuthorName, hashPassword("crawler-disabled-login"))
	return id, err
}

func seedPost(db *sqlx.DB, article seedArticle, authorID, communityID int64) (bool, error) {
	var existingID int64
	err := db.Get(
		&existingID,
		`select post_id from post where title = ? or (community_id = ? and content like ?) limit 1`,
		article.Title,
		communityID,
		"%"+article.Title+"%",
	)
	if err == nil {
		return false, redis.CreatePost(existingID, communityID)
	}
	if err != sql.ErrNoRows {
		return false, err
	}

	postID := snowflake.GenID()
	_, err = db.Exec(
		`insert into post(post_id, title, content, author_id, community_id) values (?, ?, ?, ?, ?)`,
		postID,
		truncateRunes(article.Title, 128),
		truncateRunes(article.Content, 8192),
		authorID,
		communityID,
	)
	if err != nil {
		return false, err
	}
	if err := redis.CreatePost(postID, communityID); err != nil {
		return false, err
	}
	return true, nil
}

func hashPassword(password string) string {
	h := md5.New()
	h.Write([]byte("liwenzhou.com" + password))
	return hex.EncodeToString(h.Sum(nil))
}

func firstSourceContaining(sources []crawlSource, keyword string) crawlSource {
	keyword = strings.ToLower(keyword)
	for _, source := range sources {
		if strings.Contains(strings.ToLower(source.Title), keyword) || strings.Contains(strings.ToLower(source.URL), keyword) {
			return source
		}
	}
	return crawlSource{}
}

func detectSeasonName(sources []crawlSource) string {
	for _, source := range sources {
		all := strings.ToLower(source.Title + " " + source.Description + " " + strings.Join(source.Points, " "))
		if strings.Contains(all, "space gods") {
			return "Space Gods"
		}
	}
	return "最新赛季"
}

func mergePoints(primary, secondary []string, limit int) []string {
	out := make([]string, 0, limit)
	seen := make(map[string]bool)
	for _, group := range [][]string{primary, secondary} {
		for _, item := range group {
			item = strings.TrimSpace(item)
			if item == "" || seen[item] {
				continue
			}
			seen[item] = true
			out = append(out, item)
			if len(out) >= limit {
				return out
			}
		}
	}
	return out
}

func collectGuideLinks(sources []crawlSource) []string {
	out := make([]string, 0, 3)
	seen := make(map[string]bool)
	for _, source := range sources {
		for _, link := range source.Links {
			if seen[link] {
				continue
			}
			seen[link] = true
			out = append(out, link)
		}
	}
	if len(out) == 0 {
		return []string{"官方补丁页内未发现外部阵容数据入口，可以先使用官方资料补充站内攻略。"}
	}
	return out
}

func sourceSection(sources ...crawlSource) string {
	lines := []string{"来源链接："}
	for _, source := range sources {
		if source.URL == "" {
			continue
		}
		lines = append(lines, fmt.Sprintf("- %s：%s", source.Title, source.URL))
	}
	return strings.Join(lines, "\n")
}

func bulletList(items []string) string {
	if len(items) == 0 {
		return "- 暂无可提取内容。"
	}
	lines := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		lines = append(lines, "- "+item)
	}
	if len(lines) == 0 {
		return "- 暂无可提取内容。"
	}
	return strings.Join(lines, "\n")
}

func nonEmptySlice(items []string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

func joinSections(sections ...string) string {
	out := make([]string, 0, len(sections))
	for _, section := range sections {
		section = strings.TrimSpace(section)
		if section != "" {
			out = append(out, section)
		}
	}
	return strings.Join(out, "\n\n")
}

func cleanText(value string) string {
	value = strings.ReplaceAll(value, "<br>", " ")
	value = strings.ReplaceAll(value, "<br/>", " ")
	value = strings.ReplaceAll(value, "<br />", " ")
	value = tagRe.ReplaceAllString(value, " ")
	value = html.UnescapeString(value)
	value = strings.ReplaceAll(value, "\u00a0", " ")
	value = spaceRe.ReplaceAllString(value, " ")
	return strings.TrimSpace(value)
}

func containsAny(value string, keywords []string) bool {
	for _, keyword := range keywords {
		if strings.Contains(value, strings.ToLower(keyword)) {
			return true
		}
	}
	return false
}

func truncateRunes(value string, max int) string {
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max])
}
