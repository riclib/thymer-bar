/**
 * AgentHub Streaming Renderer
 * Progressive rendering of LLM responses using promise chaining.
 */
const AgentStreaming = {
  /**
   * Normalize code language aliases
   */
  normalizeLanguage(lang) {
    if (!lang) return 'plaintext';
    const aliases = {
      'js': 'javascript', 'ts': 'typescript', 'py': 'python',
      'rb': 'ruby', 'sh': 'bash', 'yml': 'yaml',
    };
    return aliases[lang.toLowerCase()] || lang.toLowerCase();
  },

  /**
   * Create a streaming renderer for progressive LLM output
   */
  createRenderer(record, labelItem) {
    const self = this;

    return {
      record, labelItem,
      previewItem: null,
      processedLength: 0,
      buffer: '',
      inCodeBlock: false,
      codeBlockLang: '',
      renderedCount: 0,
      lastItemPromise: null,
      isFirstBlock: true,

      async init() {
        // All response items are children of the label (agent marker)
        this.lastItemPromise = Promise.resolve(null); // null = first child
        this.previewItem = await this.record.createLineItem(this.labelItem, null, 'text');
      },

      update(fullText) {
        const newText = fullText.slice(this.processedLength);
        if (!newText) return;

        for (const char of newText) {
          this.buffer += char;
          this.processedLength++;

          if (this.inCodeBlock) {
            if (this.buffer.endsWith('\n```\n') ||
                (this.buffer.endsWith('\n```') && fullText.length === this.processedLength)) {
              const endMarker = this.buffer.endsWith('\n```\n') ? '\n```\n' : '\n```';
              this.renderCodeBlock(this.codeBlockLang, this.buffer.slice(0, -endMarker.length));
              this.buffer = '';
              this.inCodeBlock = false;
              this.codeBlockLang = '';
            }
          } else if (char === '\n') {
            const line = this.buffer.slice(0, -1);
            const fenceMatch = line.match(/^(\s*)```(.*)$/);

            if (fenceMatch) {
              this.inCodeBlock = true;
              this.codeBlockLang = fenceMatch[2].trim();
              this.buffer = '';
            } else if (line.trim()) {
              this.renderLine(line);
              this.buffer = '';
            } else {
              this.buffer = '';
            }
          }

          this.updatePreview();
        }
      },

      renderLine(line) {
        const parsed = window.syncHub?.parseLine?.(line);
        if (!parsed?.segments?.length) return;

        const { type, segments, level } = parsed;
        const renderer = this;
        const isHeading = type === 'heading';
        const needsBlankLine = BLANK_LINE_BEFORE_HEADINGS && isHeading && !this.isFirstBlock;

        this.lastItemPromise = this.lastItemPromise.then(async (lastItem) => {
          try {
            let insertAfter = lastItem;

            // Add blank line before headings (except first block)
            if (needsBlankLine) {
              const blank = await renderer.record.createLineItem(renderer.labelItem, insertAfter, 'text');
              if (blank) {
                blank.setSegments([]);
                insertAfter = blank;
              }
            }

            // Create as child of labelItem (agent marker)
            const item = await renderer.record.createLineItem(renderer.labelItem, insertAfter, type);
            if (item) {
              if (isHeading && level > 1) {
                try { item.setHeadingSize?.(level); } catch(e) {}
              }
              item.setSegments(segments);
              renderer.renderedCount++;
              renderer.isFirstBlock = false;
              return item;
            }
            return lastItem;
          } catch (e) {
            return lastItem;
          }
        });
      },

      renderCodeBlock(lang, content) {
        const lines = content.split('\n');
        const renderer = this;

        this.lastItemPromise = this.lastItemPromise.then(async (lastItem) => {
          try {
            // Create code block as child of labelItem (agent marker)
            const block = await renderer.record.createLineItem(renderer.labelItem, lastItem, 'block');
            if (!block) return lastItem;

            try { block.setHighlightLanguage?.(self.normalizeLanguage(lang)); } catch(e) {}
            block.setSegments([]);

            let prev = null;
            for (const codeLine of lines) {
              const li = await renderer.record.createLineItem(block, prev, 'text');
              if (li) { li.setSegments([{ type: 'text', text: codeLine }]); prev = li; }
            }

            renderer.renderedCount++;
            renderer.isFirstBlock = false;
            return block;
          } catch (e) {
            return lastItem;
          }
        });
      },

      updatePreview() {
        if (!this.previewItem) return;
        try {
          const display = this.buffer || '';
          this.previewItem.setSegments([
            ...(display ? [{ type: this.inCodeBlock ? 'code' : 'text', text: display }] : []),
            { type: 'code', text: '\u2588' } // Block cursor
          ]);
        } catch (e) {}
      },

      async finalize() {
        if (this.buffer.trim()) {
          if (this.inCodeBlock) {
            this.renderCodeBlock(this.codeBlockLang, this.buffer);
          } else {
            this.renderLine(this.buffer);
          }
        }

        await this.lastItemPromise;

        try {
          this.previewItem?.setSegments([]);
        } catch (e) {}

        return { rendered: this.renderedCount };
      },
    };
  }
};
