/**
 * AgentHub Chat
 * Chat page parsing, message building, and link resolution.
 */
const AgentChat = {
  /**
   * Parse a chat page into messages.
   * - Agent markers (**AgentName robot:**) and their CHILDREN are assistant messages
   * - Top-level items (not children of markers) are user content
   * - Links [[...]] get resolved and inlined as context
   */
  async parseChatPage(record, dataApi) {
    const items = await record.getLineItems();

    const messages = [];
    const linkedContent = new Map();

    // Pattern: **AgentName robot:** (bold text ending with robot emoji and colon)
    const agentMarkerPattern = /^(.+)\s*\u{1F916}:$/u;

    // First pass: identify agent marker guids
    const agentMarkerGuids = new Set();
    for (const item of items) {
      if (item.parent_guid !== record.guid) continue; // Only top-level
      const firstSegment = item.segments?.[0];
      if (firstSegment?.type === 'bold') {
        const text = (item.segments || []).map(s => s.text || '').join('');
        if (agentMarkerPattern.test(text)) {
          agentMarkerGuids.add(item.guid);
        }
      }
    }

    let currentRole = null;
    let currentContent = '';

    for (const item of items) {
      const parentGuid = item.parent_guid;
      const isTopLevel = parentGuid === record.guid;
      const isAgentChild = agentMarkerGuids.has(parentGuid);

      // Extract text, including ref GUIDs as [[GUID]] so LLM sees something
      const text = (item.segments || []).map(s => {
        if (s.type === 'ref' && typeof s.text === 'object') {
          return `[[${s.text.guid}]]`;
        }
        return s.text || '';
      }).join('');

      // Skip items that are nested deeper (children of children)
      if (!isTopLevel && !isAgentChild) continue;

      if (isTopLevel) {
        // Check if this is an agent marker
        const firstSegment = item.segments?.[0];
        const isBoldMarker = firstSegment?.type === 'bold';
        const markerMatch = isBoldMarker && text.match(agentMarkerPattern);

        if (markerMatch) {
          // Save previous message
          if (currentRole && currentContent.trim()) {
            messages.push({ role: currentRole, content: currentContent.trim() });
          }
          currentRole = 'assistant';
          currentContent = '';
        } else if (text.trim()) {
          // User content - save previous and start new
          if (currentRole && currentContent.trim()) {
            messages.push({ role: currentRole, content: currentContent.trim() });
          }
          currentRole = 'user';
          currentContent = text;
        }
      } else if (isAgentChild && text.trim()) {
        // Child of agent marker = assistant content
        if (currentRole !== 'assistant') {
          if (currentRole && currentContent.trim()) {
            messages.push({ role: currentRole, content: currentContent.trim() });
          }
          currentRole = 'assistant';
          currentContent = '';
        }
        currentContent += (currentContent ? '\n' : '') + text;
      }

      // Extract linked content for context
      const links = this.extractLinks(item.segments || []);
      for (const link of links) {
        if (link.guid && !linkedContent.has(link.guid)) {
          const content = await this.resolveLink(link.guid, dataApi);
          if (content) {
            linkedContent.set(link.guid, { title: link.title, content });
          }
        }
      }
    }

    // Don't forget the last message
    if (currentRole && currentContent.trim()) {
      messages.push({ role: currentRole, content: currentContent.trim() });
    }

    console.log('[AgentHub] Parsed messages:', messages.length, 'linked:', linkedContent.size);
    return { messages, linkedContent };
  },

  /**
   * Extract linked records from segments (type: 'ref')
   */
  extractLinks(segments) {
    const links = [];
    for (const segment of segments) {
      if (segment.type === 'ref' && segment.text?.guid) {
        links.push({
          title: segment.text.title || 'Linked record',
          guid: segment.text.guid
        });
      }
    }
    return links;
  },

  /**
   * Resolve a block/page GUID to its content
   */
  async resolveLink(guid, dataApi) {
    try {
      const collections = await dataApi.getAllCollections();
      for (const collection of collections) {
        const records = await collection.getAllRecords();
        const record = records.find(r => r.guid === guid);
        if (record) {
          const items = await record.getLineItems();
          return items.map(i => (i.segments || []).map(s => {
            // Handle ref segments
            if (s.type === 'ref' && typeof s.text === 'object') {
              return s.text.title || `[[${s.text.guid}]]`;
            }
            return s.text || '';
          }).join('')).join('\n');
        }
      }
    } catch (e) {
      console.error('[AgentHub] Failed to resolve link:', e);
    }
    return null;
  },

  /**
   * Build final messages with linked content prepended
   */
  buildMessages(messages, linkedContent) {
    // Replace [[GUID]] with titles in all messages
    if (linkedContent.size > 0) {
      for (const [guid, { title }] of linkedContent) {
        const pattern = new RegExp(`\\[\\[${guid}\\]\\]`, 'g');
        for (let i = 0; i < messages.length; i++) {
          messages[i] = {
            ...messages[i],
            content: messages[i].content.replace(pattern, `"${title}"`)
          };
        }
      }
    }

    // If there's linked content, prepend it to the first user message
    if (linkedContent.size > 0 && messages.length > 0) {
      let contextBlock = '---\nLinked Context (already resolved - no need to search):\n';
      for (const [guid, { title, content }] of linkedContent) {
        contextBlock += `\n## ${title}\n${content}\n`;
      }
      contextBlock += '---\n\n';

      // Find first user message and prepend context
      const firstUserIdx = messages.findIndex(m => m.role === 'user');
      if (firstUserIdx >= 0) {
        messages[firstUserIdx] = {
          ...messages[firstUserIdx],
          content: contextBlock + messages[firstUserIdx].content,
        };
      }
    }

    return messages;
  }
};
