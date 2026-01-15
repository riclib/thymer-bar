/**
 * SyncHub Markdown Utilities
 * Parsing and insertion of markdown content into Thymer records.
 */
const SyncHubMarkdown = {
  /**
   * Simple hash function for content comparison.
   */
  hashContent(text) {
    let hash = 0;
    for (let i = 0; i < text.length; i++) {
      hash = (hash << 5) - hash + text.charCodeAt(i);
      hash |= 0;
    }
    return hash.toString(36);
  },

  /**
   * Replace all contents of a record with new markdown.
   * Deletes all existing line items and inserts fresh content.
   */
  async replace(markdown, record) {
    if (!record) {
      console.warn('[SyncHub] replace: No record provided');
      return 0;
    }

    // Get all existing line items
    const existingItems = await record.getLineItems();

    // Delete all items - children first, then parents (leaf-to-root order)
    // Sort by depth (items with no children in existingItems go first)
    const itemsWithChildren = new Set();
    for (const item of existingItems) {
      if (item.parent_guid && item.parent_guid !== record.guid) {
        itemsWithChildren.add(item.parent_guid);
      }
    }

    // Delete leaf items first
    for (const item of existingItems) {
      if (!itemsWithChildren.has(item.guid)) {
        try { await item.delete(); } catch (e) {}
      }
    }

    // Then delete remaining (parents)
    for (const item of existingItems) {
      if (itemsWithChildren.has(item.guid)) {
        try { await item.delete(); } catch (e) {}
      }
    }

    // Insert fresh content
    const count = await this.insert(markdown, record, null);
    console.log('[SyncHub] Replaced content:', count, 'lines');
    return count;
  },

  /**
   * Parse markdown into line data for replaceContents.
   */
  parseMarkdownToLines(markdown) {
    const lines = [];
    let inCode = false, codeLang = '', codeLines = [];
    let isFirstBlock = true;

    for (const line of markdown.split('\n')) {
      const fenceMatch = line.match(/^(\s*)```(.*)$/);
      if (fenceMatch) {
        if (inCode) {
          lines.push({
            type: 'block',
            segments: [{ type: 'text', text: codeLines.join('\n') }],
            lang: codeLang,
          });
          inCode = false;
          codeLang = '';
          codeLines = [];
          isFirstBlock = false;
        } else {
          inCode = true;
          codeLang = fenceMatch[2].trim();
        }
        continue;
      }

      if (inCode) {
        codeLines.push(line);
        continue;
      }

      if (!line.trim()) continue;

      const parsed = this.parseLine(line);
      if (parsed) {
        // Add blank line before headings (except first block)
        if (parsed.type === 'heading' && !isFirstBlock) {
          lines.push({ type: 'text', segments: [] });
        }
        lines.push(parsed);
        isFirstBlock = false;
      }
    }

    return lines;
  },

  /**
   * Insert markdown content into a record after a specific item.
   */
  async insert(markdown, record, afterItem = null) {
    if (!record) return 0;

    let promise = Promise.resolve(afterItem);
    let rendered = 0;
    let inCode = false, codeLang = '', codeLines = [];
    let isFirstBlock = true;

    for (const line of markdown.split('\n')) {
      const fenceMatch = line.match(/^(\s*)```(.*)$/);
      if (fenceMatch) {
        if (inCode) {
          const lang = codeLang, code = [...codeLines];
          promise = promise.then(async (last) => {
            const block = await record.createLineItem(null, last, 'block');
            if (!block) return last;
            try { block.setHighlightLanguage?.(this.normalizeLanguage(lang)); } catch (e) {}
            block.setSegments([]);
            let prev = null;
            for (const cl of code) {
              const li = await record.createLineItem(block, prev, 'text');
              if (li) { li.setSegments([{ type: 'text', text: cl }]); prev = li; }
            }
            rendered++;
            return block;
          });
          isFirstBlock = false;
          inCode = false; codeLang = ''; codeLines = [];
        } else {
          inCode = true; codeLang = fenceMatch[2].trim();
        }
        continue;
      }

      if (inCode) { codeLines.push(line); continue; }
      if (!line.trim()) continue;

      const parsed = this.parseLine(line);
      if (!parsed) continue;

      const { type, segments, level } = parsed;
      const isHeading = type === 'heading';
      const needsBlankLine = isHeading && !isFirstBlock;

      promise = promise.then(async (last) => {
        let insertAfter = last;

        // Add blank line before headings (except first block)
        if (needsBlankLine) {
          const blank = await record.createLineItem(null, insertAfter, 'text');
          if (blank) {
            blank.setSegments([]);
            insertAfter = blank;
          }
        }

        const item = await record.createLineItem(null, insertAfter, type);
        if (!item) return last;
        if (isHeading && level > 1) {
          try { item.setHeadingSize?.(level); } catch (e) {}
        }
        item.setSegments(segments);
        rendered++;
        return item;
      });

      isFirstBlock = false;
    }

    await promise;
    return rendered;
  },

  /**
   * Parse a single line of markdown into type and segments.
   */
  parseLine(line) {
    if (!line.trim()) return null;

    // Horizontal rule
    if (/^(\*\s*\*\s*\*|-\s*-\s*-|_\s*_\s*_)[\s*\-_]*$/.test(line.trim())) {
      return { type: 'br', segments: [] };
    }

    // Headings
    const hMatch = line.match(/^(#{1,6})\s+(.+)$/);
    if (hMatch) {
      return {
        type: 'heading',
        level: hMatch[1].length,
        segments: this.parseInline(hMatch[2]),
      };
    }

    // Task list
    const taskMatch = line.match(/^(\s*)[-*]\s+\[([ xX])\]\s+(.+)$/);
    if (taskMatch) {
      return { type: 'task', segments: this.parseInline(taskMatch[3]) };
    }

    // Unordered list
    const ulMatch = line.match(/^(\s*)[-*]\s+(.+)$/);
    if (ulMatch) {
      return { type: 'ulist', segments: this.parseInline(ulMatch[2]) };
    }

    // Ordered list
    const olMatch = line.match(/^(\s*)\d+\.\s+(.+)$/);
    if (olMatch) {
      return { type: 'olist', segments: this.parseInline(olMatch[2]) };
    }

    // Quote
    if (line.startsWith('> ')) {
      return { type: 'quote', segments: this.parseInline(line.slice(2)) };
    }

    // Regular text
    return { type: 'text', segments: this.parseInline(line) };
  },

  /**
   * Parse inline formatting (bold, italic, code, links, refs).
   */
  parseInline(text) {
    const segments = [];
    const patterns = [
      { regex: /`([^`]+)`/, type: 'code' },
      { regex: /\[\[([A-Za-z0-9-]{20,})\]\]/, type: 'ref' },
      { regex: /\[([^\]]+)\]\(([^)]+)\)/, type: 'link' },
      { regex: /\*\*([^*]+)\*\*/, type: 'bold' },
      { regex: /__([^_]+)__/, type: 'bold' },
      { regex: /\*([^*]+)\*/, type: 'italic' },
    ];

    let remaining = text;

    while (remaining.length > 0) {
      let earliestMatch = null;
      let earliestIndex = remaining.length;
      let matchedPattern = null;

      for (const pattern of patterns) {
        const match = remaining.match(pattern.regex);
        if (match && match.index < earliestIndex) {
          earliestMatch = match;
          earliestIndex = match.index;
          matchedPattern = pattern;
        }
      }

      if (earliestMatch && matchedPattern) {
        if (earliestIndex > 0) {
          segments.push({ type: 'text', text: remaining.slice(0, earliestIndex) });
        }

        if (matchedPattern.type === 'link') {
          segments.push({ type: 'text', text: earliestMatch[1] });
        } else if (matchedPattern.type === 'ref') {
          segments.push({ type: 'ref', text: { guid: earliestMatch[1] } });
        } else {
          segments.push({ type: matchedPattern.type, text: earliestMatch[1] });
        }

        remaining = remaining.slice(earliestIndex + earliestMatch[0].length);
      } else {
        segments.push({ type: 'text', text: remaining });
        break;
      }
    }

    return segments.length ? segments : [{ type: 'text', text }];
  },

  /**
   * Normalize language aliases for code blocks.
   */
  normalizeLanguage(lang) {
    if (!lang) return 'plaintext';
    const aliases = {
      js: 'javascript', ts: 'typescript', py: 'python', rb: 'ruby',
      sh: 'bash', yml: 'yaml', 'c++': 'cpp', 'c#': 'csharp', cs: 'csharp',
      golang: 'go', rs: 'rust', kt: 'kotlin', md: 'markdown',
    };
    return aliases[lang.toLowerCase()] || lang.toLowerCase();
  },
};
