package cmd

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var addCmd = &cobra.Command{
	Use:   "add [text]",
	Short: "Quick capture to lifelog",
	Long: `Capture text or files to your lifelog.

Examples:
  llog add "quick thought"              # one-liner to daily note
  llog add "Title" -m "more details"    # multi-line to daily note
  echo "piped text" | llog add          # from stdin
  cat spec.md | llog add                # file with frontmatter to collection`,
	Run: runAdd,
}

var addMessage string

func init() {
	rootCmd.AddCommand(addCmd)
	addCmd.Flags().StringVarP(&addMessage, "message", "m", "", "additional lines (multi-line capture)")
}

func runAdd(cmd *cobra.Command, args []string) {
	// Get lifelog dir
	home, _ := os.UserHomeDir()
	lifelogDir := filepath.Join(home, "lifelog")

	// Read input from args or stdin
	var input string
	if len(args) > 0 {
		input = strings.Join(args, " ")
		if addMessage != "" {
			input = input + "\n" + addMessage
		}
	} else {
		// Read from stdin
		stat, _ := os.Stdin.Stat()
		if (stat.Mode() & os.ModeCharDevice) == 0 {
			bytes, err := io.ReadAll(os.Stdin)
			if err != nil {
				fmt.Printf("Error reading stdin: %v\n", err)
				return
			}
			input = string(bytes)
		} else {
			fmt.Println("Usage: llog add \"text\" or echo \"text\" | llog add")
			return
		}
	}

	input = strings.TrimSpace(input)
	if input == "" {
		fmt.Println("Nothing to add")
		return
	}

	now := time.Now()
	timestamp := now.Format("15:04")

	// Check if input has frontmatter
	if strings.HasPrefix(input, "---") {
		handleFrontmatterInput(input, lifelogDir, now, timestamp)
		return
	}

	// Plain text - append to daily note
	handlePlainTextInput(input, lifelogDir, now, timestamp)
}

func handlePlainTextInput(input, lifelogDir string, now time.Time, timestamp string) {
	lines := strings.Split(input, "\n")

	var entry string
	if len(lines) == 1 {
		// One-liner
		entry = fmt.Sprintf("**%s** %s", timestamp, lines[0])
	} else {
		// Multi-line (indent subsequent lines, blank line after)
		entry = fmt.Sprintf("**%s** %s", timestamp, lines[0])
		for _, line := range lines[1:] {
			entry += fmt.Sprintf("\n\t%s", line)
		}
		entry += "\n" // blank line after multi-line
	}

	if err := appendToDaily(lifelogDir, now, entry); err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("Added to daily note\n")
}

func handleFrontmatterInput(input, lifelogDir string, now time.Time, timestamp string) {
	// Parse frontmatter
	parts := strings.SplitN(input, "---", 3)
	if len(parts) < 3 {
		fmt.Println("Invalid frontmatter format")
		return
	}

	var fm map[string]any
	if err := yaml.Unmarshal([]byte(parts[1]), &fm); err != nil {
		fmt.Printf("Error parsing frontmatter: %v\n", err)
		return
	}

	content := strings.TrimSpace(parts[2])

	// Get title from frontmatter or first heading
	title := inferTitle(fm, content)

	// Get type from frontmatter
	typ, _ := fm["type"].(string)
	folder := typeToCollectionFolder(typ)

	var filename string
	var destDir string

	if folder != "" {
		// Known type - use collection folder
		destDir = filepath.Join(lifelogDir, folder)
	} else {
		// Unknown type - use Inbox
		destDir = filepath.Join(lifelogDir, "Inbox")
	}

	// Filename from title (must match for [[wikilinks]] to work)
	filename = sanitizeForFilename(title) + ".md"

	// Ensure directory exists
	if err := os.MkdirAll(destDir, 0755); err != nil {
		fmt.Printf("Error creating directory: %v\n", err)
		return
	}

	// Write file
	destPath := filepath.Join(destDir, filename)
	if err := os.WriteFile(destPath, []byte(input), 0644); err != nil {
		fmt.Printf("Error writing file: %v\n", err)
		return
	}

	// Add link to daily note
	linkType := typ
	if linkType == "" {
		linkType = "note"
	}
	entry := fmt.Sprintf("**%s** %s [[%s]]", timestamp, linkType, title)
	if err := appendToDaily(lifelogDir, now, entry); err != nil {
		fmt.Printf("Error updating daily note: %v\n", err)
		return
	}

	fmt.Printf("Added to %s/%s\n", filepath.Base(destDir), filename)
}

func inferTitle(fm map[string]any, content string) string {
	// Try frontmatter title first
	if title, ok := fm["title"].(string); ok && title != "" {
		return title
	}

	// Look for first heading in content
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			return strings.TrimPrefix(line, "# ")
		}
	}

	// Fallback to first line
	if len(lines) > 0 && lines[0] != "" {
		title := lines[0]
		if len(title) > 50 {
			title = title[:50]
		}
		return title
	}

	return "Untitled"
}

func typeToCollectionFolder(typ string) string {
	switch typ {
	case "issue":
		return "Issues"
	case "pr":
		return "PRs"
	case "capture":
		return "Captures"
	case "highlight":
		return "Highlights"
	case "event":
		return "Events"
	default:
		return "" // Unknown - will go to Inbox
	}
}

func sanitizeForFilename(s string) string {
	// Replace problematic characters
	replacer := strings.NewReplacer(
		"/", "-",
		"\\", "-",
		":", "-",
		"*", "",
		"?", "",
		"\"", "",
		"<", "",
		">", "",
		"|", "",
	)
	result := replacer.Replace(s)
	// Trim and limit length
	result = strings.TrimSpace(result)
	if len(result) > 100 {
		result = result[:100]
	}
	return result
}

func appendToDaily(lifelogDir string, now time.Time, entry string) error {
	dailyDir := filepath.Join(lifelogDir, "Daily")
	if err := os.MkdirAll(dailyDir, 0755); err != nil {
		return err
	}

	dailyPath := filepath.Join(dailyDir, now.Format("2006-01-02")+".md")

	// Open file for appending (create if doesn't exist)
	f, err := os.OpenFile(dailyPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	// Check if file is empty or doesn't end with newline
	stat, _ := f.Stat()
	if stat.Size() > 0 {
		// Read last byte to check for newline
		f2, _ := os.Open(dailyPath)
		defer f2.Close()
		buf := make([]byte, 1)
		f2.Seek(-1, io.SeekEnd)
		f2.Read(buf)
		if buf[0] != '\n' {
			f.WriteString("\n")
		}
	}

	_, err = f.WriteString(entry + "\n")
	return err
}

// For reading multiline from stdin interactively (future enhancement)
func readMultiline() string {
	var lines []string
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return strings.Join(lines, "\n")
}
