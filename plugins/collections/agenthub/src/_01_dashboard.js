/**
 * AgentHub Dashboard
 * Dashboard view for agent status and token usage.
 */
const AgentDashboard = {
  /**
   * Format number with k/M/G suffix
   */
  formatTokens(num) {
    if (num === 0) return '0';
    if (num < 1000) return num.toString();
    if (num < 1000000) return (num / 1000).toFixed(1) + 'k';
    if (num < 1000000000) return (num / 1000000).toFixed(1) + 'M';
    return (num / 1000000000).toFixed(1) + 'G';
  },

  /**
   * Format relative time
   */
  formatRelativeTime(date) {
    if (!date) return 'Never';
    const now = new Date();
    const diff = now - date;
    const minutes = Math.floor(diff / 60000);
    const hours = Math.floor(diff / 3600000);
    const days = Math.floor(diff / 86400000);

    if (minutes < 1) return 'Just now';
    if (minutes < 60) return `${minutes}m ago`;
    if (hours < 24) return `${hours}h ago`;
    if (days < 7) return `${days}d ago`;
    return date.toLocaleDateString();
  },

  /**
   * Escape HTML to prevent XSS
   */
  escapeHtml(text) {
    if (!text) return '';
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
  },

  /**
   * Render a single agent card
   */
  renderAgentCard(record, viewContext) {
    const name = record.getName() || 'Unnamed Agent';
    const enabled = record.prop('enabled')?.choice();
    const status = record.prop('status')?.choice() || 'idle';
    const lastRun = record.prop('last_run')?.date();
    const inputTokens = record.prop('input_tokens')?.number() || 0;
    const outputTokens = record.prop('output_tokens')?.number() || 0;
    const generationMs = record.prop('total_generation_ms')?.number() || 0;
    const invocations = record.prop('invocations')?.number() || 0;
    const lastError = record.prop('last_error')?.text();
    const provider = record.prop('provider')?.choice() || 'anthropic';
    const model = record.prop('model')?.choice() || 'sonnet';

    const totalTokens = inputTokens + outputTokens;
    const tps = generationMs > 0 ? (outputTokens / (generationMs / 1000)) : 0;

    // Determine status dot
    let dotClass = '';
    let dotContent = '';
    if (enabled === 'no') {
      dotClass = 'disabled';
    } else if (status === 'thinking') {
      dotClass = 'thinking';
      dotContent = '<span class="ti ti-blinking-dot"></span>';
    } else if (status === 'error') {
      dotClass = 'error';
    }

    // Provider icon
    const providerIcons = {
      'anthropic': 'brand-anthropic',
      'openai': 'brand-openai',
      'ollama': 'box',
      'custom': 'plug'
    };
    const icon = providerIcons[provider] || 'robot';

    const card = document.createElement('div');
    card.className = 'agent-card';
    card.setAttribute('data-status', status);
    card.setAttribute('data-enabled', enabled || 'no');

    const timeText = lastRun ? this.formatRelativeTime(new Date(lastRun)) : 'Never used';

    card.innerHTML = `
      <div class="agent-card-header">
        <span class="agent-card-icon ti ti-${icon}"></span>
        <span class="agent-card-name">${this.escapeHtml(name)}</span>
        <span class="agent-card-dot ${dotClass}">${dotContent}</span>
      </div>
      <div class="agent-card-body">
        <div class="agent-card-value">${this.formatTokens(totalTokens)}</div>
        <div class="agent-card-label">tokens</div>
        ${totalTokens > 0 ? `
          <div class="agent-card-breakdown">
            <span class="up">up ${this.formatTokens(outputTokens)}</span>
            <span class="down">down ${this.formatTokens(inputTokens)}</span>
            ${tps > 0 ? `<span class="tps">~${tps.toFixed(0)} tps</span>` : ''}
          </div>
        ` : ''}
      </div>
      <div class="agent-card-footer">
        <div class="agent-card-time">${this.escapeHtml(timeText)} - ${this.escapeHtml(model)}</div>
        ${invocations > 0 ? `<div class="agent-card-invocations">${invocations} chat${invocations !== 1 ? 's' : ''}</div>` : ''}
        ${status === 'error' && lastError ?
          `<div class="agent-card-error" title="${this.escapeHtml(lastError)}">${this.escapeHtml(lastError.substring(0, 40))}...</div>`
          : ''}
      </div>
    `;

    // Click to open agent config
    card.addEventListener('click', () => {
      viewContext.openRecordInOtherPanel(record.guid);
    });

    return {
      card,
      enabled: enabled === 'yes',
      inputTokens,
      outputTokens,
      invocations,
      generationMs
    };
  },

  /**
   * Render the full dashboard
   */
  render(container, records, viewContext) {
    container.innerHTML = '';

    // Filter to agents with names
    const agents = records.filter(r => r.getName());

    if (agents.length === 0) {
      container.innerHTML = `
        <div class="agent-dashboard-empty">
          <div class="agent-dashboard-empty-icon">robot</div>
          <div class="agent-dashboard-empty-title">No Agents</div>
          <div>Create an agent to see it here</div>
        </div>
      `;
      return;
    }

    // Create card grid
    const grid = document.createElement('div');
    grid.className = 'agent-dashboard-grid';

    let enabledCount = 0;
    let totalInputTokens = 0;
    let totalOutputTokens = 0;
    let totalInvocations = 0;
    let totalGenerationMs = 0;

    agents.forEach(record => {
      const result = this.renderAgentCard(record, viewContext);
      grid.appendChild(result.card);

      if (result.enabled) {
        enabledCount++;
        totalInputTokens += result.inputTokens;
        totalOutputTokens += result.outputTokens;
        totalInvocations += result.invocations;
        totalGenerationMs += result.generationMs;
      }
    });

    container.appendChild(grid);

    // Summary row
    const totalTokensAll = totalInputTokens + totalOutputTokens;
    const overallTps = totalGenerationMs > 0 ? (totalOutputTokens / (totalGenerationMs / 1000)) : 0;
    const summary = document.createElement('div');
    summary.className = 'agent-dashboard-summary';
    summary.innerHTML = `
      <div class="agent-summary-item">
        <span class="agent-summary-value">${enabledCount}</span>
        <span>agent${enabledCount !== 1 ? 's' : ''} active</span>
      </div>
      <div class="agent-summary-item">
        <span class="agent-summary-value">${this.formatTokens(totalTokensAll)}</span>
        <span>total tokens</span>
      </div>
      <div class="agent-summary-item">
        <span class="up">up ${this.formatTokens(totalOutputTokens)}</span>
        <span class="down">down ${this.formatTokens(totalInputTokens)}</span>
      </div>
      ${overallTps > 0 ? `
        <div class="agent-summary-item">
          <span class="tps">~${overallTps.toFixed(0)} tps</span>
        </div>
      ` : ''}
      <div class="agent-summary-item">
        <span class="agent-summary-value">${totalInvocations}</span>
        <span>chats</span>
      </div>
    `;
    container.appendChild(summary);
  }
};
