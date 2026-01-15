package cmd

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	md "github.com/JohannesKaufmann/html-to-markdown"
	readability "github.com/go-shiori/go-readability"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var urlCmd = &cobra.Command{
	Use:   "url <url>",
	Short: "Capture a web page to Captures",
	Long: `Fetch a URL, extract readable content, convert to markdown, and save.

Examples:
  llog url https://example.com/article
  llog url https://news.ycombinator.com/item?id=12345`,
	Args: cobra.ExactArgs(1),
	Run:  runURL,
}

var urlTags string

func init() {
	rootCmd.AddCommand(urlCmd)
	urlCmd.Flags().StringVarP(&urlTags, "tags", "t", "", "comma-separated tags")
}

func runURL(cmd *cobra.Command, args []string) {
	targetURL := args[0]

	// Validate URL
	parsed, err := url.Parse(targetURL)
	if err != nil || parsed.Scheme == "" {
		fmt.Printf("Invalid URL: %s\n", targetURL)
		return
	}

	fmt.Printf("Fetching %s...\n", targetURL)

	// Fetch and parse with readability
	article, err := readability.FromURL(targetURL, 30*time.Second)
	if err != nil {
		fmt.Printf("Error fetching URL: %v\n", err)
		return
	}

	// Convert HTML content to markdown
	converter := md.NewConverter("", true, nil)
	markdown, err := converter.ConvertString(article.Content)
	if err != nil {
		fmt.Printf("Error converting to markdown: %v\n", err)
		return
	}

	// Clean up markdown
	markdown = strings.TrimSpace(markdown)

	// Build frontmatter
	now := time.Now()
	fm := map[string]any{
		"type":         "capture",
		"source":       "web",
		"title":        article.Title,
		"url":          targetURL,
		"site":         parsed.Host,
		"captured_at":  now.Format(time.RFC3339),
	}

	if article.Byline != "" {
		fm["author"] = article.Byline
	}
	if article.SiteName != "" {
		fm["site_name"] = article.SiteName
	}
	if article.Excerpt != "" {
		fm["excerpt"] = article.Excerpt
	}
	if article.PublishedTime != nil && !article.PublishedTime.IsZero() {
		fm["published"] = article.PublishedTime.Format(time.RFC3339)
	}
	if article.Image != "" {
		fm["image"] = article.Image
	}
	if urlTags != "" {
		fm["tags"] = strings.Split(urlTags, ",")
	}

	// Marshal frontmatter
	fmBytes, err := yaml.Marshal(fm)
	if err != nil {
		fmt.Printf("Error creating frontmatter: %v\n", err)
		return
	}

	// Build full content
	var content strings.Builder
	content.WriteString("---\n")
	content.Write(fmBytes)
	content.WriteString("---\n\n")
	content.WriteString(fmt.Sprintf("# %s\n\n", article.Title))
	content.WriteString(markdown)

	// Save to Captures folder
	home, _ := os.UserHomeDir()
	capturesDir := filepath.Join(home, "lifelog", "Captures")
	if err := os.MkdirAll(capturesDir, 0755); err != nil {
		fmt.Printf("Error creating directory: %v\n", err)
		return
	}

	// Filename from title (must match for [[wikilinks]] to work)
	filename := sanitizeForFilename(article.Title) + ".md"
	destPath := filepath.Join(capturesDir, filename)

	if err := os.WriteFile(destPath, []byte(content.String()), 0644); err != nil {
		fmt.Printf("Error writing file: %v\n", err)
		return
	}

	// Add link to daily note
	timestamp := now.Format("15:04")
	entry := fmt.Sprintf("**%s** capture [[%s]]", timestamp, article.Title)
	if err := appendToDaily(filepath.Join(home, "lifelog"), now, entry); err != nil {
		fmt.Printf("Error updating daily note: %v\n", err)
		return
	}

	fmt.Printf("Captured: %s\n", article.Title)
	fmt.Printf("Saved to: Captures/%s\n", filename)
}
