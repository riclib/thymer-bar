/**
 * AgentHub - AI agents that live in your Thymer space
 *
 * Each agent is a page in the AgentHub collection.
 * System prompt in a toggle at top, chat below.
 * Links bring context into the conversation.
 */
class Plugin extends CollectionPlugin {
  async onLoad() {
    this.agents = new Map();
    this.commands = [];

    // Wait for SyncHub to be ready (provides markdown utilities)
    if (!window.syncHub) {
      window.addEventListener('synchub-ready', () => this.initialize(), { once: true });
      // Timeout fallback
      setTimeout(() => {
        if (!this.initialized) this.initialize();
      }, 2000);
    } else {
      this.initialize();
    }
  }

  async initialize() {
    if (this.initialized) return;
    this.initialized = true;

    console.log('[AgentHub] Initializing ' + AGENTHUB_VERSION + (window.syncHub ? ' (SyncHub ready)' : ' (standalone)'));

    // Register with SyncHub for health tracking
    if (window.syncHub?.registerHub) {
      window.syncHub.registerHub({
        id: 'agenthub',
        name: 'AgentHub',
        version: AGENTHUB_VERSION
      });
    }

    // Load agents and register commands
    await this.loadAgents();

    // Register dashboard view
    this.registerDashboardView();
  }

  onUnload() {
    if (this.commands) {
      for (const cmd of this.commands) {
        cmd.remove();
      }
      this.commands = [];
    }
  }

  // =========================================================================
  // Agent Loading
  // =========================================================================

  async loadAgents(quiet = false) {
    try {
      const collections = await this.data.getAllCollections();
      const agentHub = collections.find(c => c.getName() === 'AgentHub');

      if (!agentHub) {
        if (!quiet) console.log('[AgentHub] Collection not found');
        return;
      }

      const records = await agentHub.getAllRecords();

      // Clear old commands
      for (const cmd of this.commands) {
        cmd.remove();
      }
      this.commands = [];
      this.agents.clear();

      // Register each enabled agent
      for (const record of records) {
        const enabled = record.prop('enabled')?.choice();
        if (enabled !== 'yes') continue;

        const name = record.getName();
        const command = record.text('command') || `@${name.toLowerCase().replace(/\s+/g, '-')}`;
        const provider = record.prop('provider')?.choice() || 'anthropic';
        const model = record.prop('model')?.choice() || 'sonnet';
        const systemPrompt = record.text('system_prompt') || '';
        const customModel = record.text('custom_model');
        const customEndpoint = record.text('custom_endpoint');
        const token = record.text('token');

        this.agents.set(record.guid, {
          guid: record.guid,
          name,
          command,
          provider,
          model,
          systemPrompt,
          customModel,
          customEndpoint,
          token,
        });

        // Register command palette command
        const cmd = this.ui.addCommandPaletteCommand({
          label: `Chat with ${name}`,
          icon: 'robot',
          onSelected: () => this.openAgentChat(record.guid),
        });
        this.commands.push(cmd);
      }

      if (!quiet) {
        console.log(`[AgentHub] Loaded ${this.agents.size} agents`);
      }
    } catch (e) {
      console.error('[AgentHub] Failed to load agents:', e);
    }
  }

  // =========================================================================
  // Dashboard View
  // =========================================================================

  registerDashboardView() {
    this.views.register("Dashboard", (viewContext) => {
      const element = viewContext.getElement();
      let records = [];
      let container = null;

      return {
        onLoad: () => {
          viewContext.makeWideLayout();
          element.style.overflow = 'auto';
          container = document.createElement('div');
          container.className = 'agent-dashboard';
          element.appendChild(container);
        },
        onRefresh: ({ records: newRecords }) => {
          records = newRecords;
          AgentDashboard.render(container, records, viewContext);
        },
        onPanelResize: () => {},
        onDestroy: () => {
          container = null;
          records = [];
        },
        onFocus: () => {},
        onBlur: () => {},
        onKeyboardNavigation: () => {}
      };
    });
  }

  // =========================================================================
  // Chat Interface
  // =========================================================================

  async openAgentChat(agentGuid) {
    console.log('[AgentHub] openAgentChat called with:', agentGuid);
    await this.runCompletion(agentGuid);
  }

  async runCompletion(agentGuid) {
    console.log('[AgentHub] runCompletion started for:', agentGuid);

    const agent = this.agents.get(agentGuid);
    if (!agent) {
      console.error('[AgentHub] Agent not found in map:', agentGuid);
      return;
    }

    // Get the ACTIVE record (where user is), not the agent's config page
    const activeRecord = this.ui.getActivePanel()?.getActiveRecord();
    if (!activeRecord) {
      console.error('[AgentHub] No active record');
      this.ui.addToaster({
        title: agent.name,
        message: 'Open a page first to chat',
        dismissible: true,
        autoDestroyTime: 3000,
      });
      return;
    }

    // Get agent config record for status updates
    const collections = await this.data.getAllCollections();
    const agentHub = collections.find(c => c.getName() === 'AgentHub');
    const agentRecords = await agentHub.getAllRecords();
    const agentRecord = agentRecords.find(r => r.guid === agentGuid);

    // Set status to Thinking
    agentRecord?.prop('status')?.setChoice('thinking');

    // Build system prompt
    const userSystemPrompt = agent.systemPrompt || '';
    const systemPrompt = userSystemPrompt
      ? `${BASE_SYSTEM_PROMPT}\n\n---\nAdditional instructions:\n${userSystemPrompt}`
      : BASE_SYSTEM_PROMPT;

    // Parse chat page
    const { messages, linkedContent } = await AgentChat.parseChatPage(activeRecord, this.data);

    if (messages.length === 0) {
      agentRecord?.prop('status')?.setChoice('idle');
      this.ui.addToaster({
        title: agent.name,
        message: 'Write something first',
        dismissible: true,
        autoDestroyTime: 3000,
      });
      return;
    }

    // Get API key
    const apiKey = agent.token || await this.getSharedApiKey();

    if (!apiKey && agent.provider !== 'ollama' && agent.provider !== 'custom') {
      agentRecord?.prop('status')?.setChoice('error');
      agentRecord?.prop('last_error')?.set('No API key configured');
      this.ui.addToaster({
        title: agent.name,
        message: 'No API key configured',
        dismissible: true,
        autoDestroyTime: 3000,
      });
      return;
    }

    // Build full messages with linked content
    const fullMessages = AgentChat.buildMessages(messages, linkedContent);

    // Find the LAST item on the page to append after
    const allItems = await activeRecord.getLineItems();
    const topLevelItems = allItems.filter(item => item.parent_guid === activeRecord.guid);
    const lastItem = topLevelItems[topLevelItems.length - 1] || null;

    // Add blank line after last content
    const blankItem = await activeRecord.createLineItem(null, lastItem, 'text');
    blankItem?.setSegments([]);

    // Create label item for agent response with robot marker
    const labelItem = await activeRecord.createLineItem(null, blankItem, 'text');
    labelItem?.setSegments([{ type: 'bold', text: `${agent.name} \u{1F916}:` }]);

    // Initialize streaming renderer
    const renderer = AgentStreaming.createRenderer(activeRecord, labelItem);
    await renderer.init();

    // Call LLM API with streaming
    try {
      const startTime = performance.now();
      const llmResult = await AgentProviders.callLLMStreaming(
        agent,
        apiKey,
        systemPrompt,
        fullMessages,
        (text) => renderer.update(text)
      );
      const generationMs = Math.round(performance.now() - startTime);

      // Finalize rendering
      const renderResult = await renderer.finalize();
      console.log('[AgentHub] Rendered:', renderResult.rendered, 'items');

      // Add blank line for user's next message
      const nextInputLine = await activeRecord.createLineItem(null, labelItem, 'text');
      nextInputLine?.setSegments([]);

      // Update stats and set status back to Idle
      await this.updateAgentStats(agentRecord, agent.name, llmResult?.usage, generationMs, activeRecord);
      agentRecord?.prop('status')?.setChoice('idle');

      // Auto-generate title if page is untitled
      const currentName = activeRecord.getName()?.trim() || '';
      const isUntitled = !currentName ||
                         currentName.toLowerCase().startsWith('untitled') ||
                         currentName.toLowerCase().startsWith('new chat');
      if (isUntitled) {
        this.suggestTitle(agent, apiKey, fullMessages, activeRecord);
      }

    } catch (e) {
      console.error('[AgentHub] API error:', e);
      agentRecord?.prop('status')?.setChoice('error');
      agentRecord?.prop('last_error')?.set(e.message);

      renderer.previewItem?.setSegments([
        { type: 'text', text: `Error: ${e.message}` },
      ]);
    }
  }

  // =========================================================================
  // Title Suggestion
  // =========================================================================

  async suggestTitle(agent, apiKey, messages, record) {
    try {
      const conversation = messages.map(m =>
        `${m.role === 'user' ? 'User' : 'Assistant'}: ${m.content.slice(0, 200)}`
      ).join('\n');

      const titlePrompt = `Based on this conversation, suggest a very short title (3-5 words max, no quotes, no punctuation at end):

${conversation}

Title:`;

      const title = await AgentProviders.quickCompletion(agent, apiKey, titlePrompt);

      if (title && title.length < 50) {
        const cleanTitle = title.trim()
          .replace(/<\|.*?\|>/g, '')
          .replace(/^["']|["']$/g, '')
          .replace(/[.!?]$/, '')
          .trim();
        if (cleanTitle) record.prop('title')?.set(cleanTitle);
      }
    } catch (e) {
      // Silent fail - this is just a delight feature
    }
  }

  // =========================================================================
  // Stats
  // =========================================================================

  async updateAgentStats(record, agentName, usage = null, generationMs = 0, chatRecord = null) {
    try {
      const count = record.prop('invocations')?.number() || 0;
      record.prop('invocations')?.set(count + 1);

      if (usage) {
        const inputTokens = record.prop('input_tokens')?.number() || 0;
        const outputTokens = record.prop('output_tokens')?.number() || 0;
        record.prop('input_tokens')?.set(inputTokens + (usage.input_tokens || 0));
        record.prop('output_tokens')?.set(outputTokens + (usage.output_tokens || 0));
      }

      if (generationMs > 0) {
        const totalMs = record.prop('total_generation_ms')?.number() || 0;
        record.prop('total_generation_ms')?.set(totalMs + generationMs);
      }

      if (window.syncHub?.setLastRun) {
        window.syncHub.setLastRun(record);
      } else if (typeof DateTime !== 'undefined') {
        record.prop('last_run')?.set(new DateTime(new Date()).value());
      }

      // Log to journal if available
      if (window.syncHub?.logToJournal && chatRecord) {
        const chatTitle = chatRecord.getName()?.trim() || 'Untitled Chat';
        await window.syncHub.logToJournal([{
          verb: `chatted with ${agentName} about`,
          guid: chatRecord.guid,
          major: false,
        }], 'verbose');
      }
    } catch (e) {
      // Stats update is non-critical
    }
  }

  async getSharedApiKey() {
    try {
      const collections = await this.data.getAllCollections();
      const syncHub = collections.find(c => c.getName() === 'Sync Hub');
      if (syncHub) {
        const records = await syncHub.getAllRecords();
        const config = records.find(r => r.text('plugin_id') === 'agenthub');
        if (config) {
          return config.text('token');
        }
      }
    } catch (e) {
      console.error('[AgentHub] Failed to get shared API key:', e);
    }
    return null;
  }
}
