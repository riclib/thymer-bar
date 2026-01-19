/**
 * PlannerHub - Simplified planning sidebar
 *
 * 3 columns: TODAY'S PLAN | DOING | NEXT
 * - Click issue → adds "- [ ] work on [[uuid]]" to daily note
 * - No sorting, no PlannerHub section - brain does the sorting
 */

const PLANNER_CSS = `
    .planner-overlay {
        position: fixed;
        top: 0;
        left: 0;
        right: 0;
        bottom: 0;
        background: rgba(0, 0, 0, 0.5);
        display: flex;
        align-items: center;
        justify-content: center;
        z-index: 9999;
    }

    .planner-panel {
        background: var(--bg-default, #1e1e1e);
        border-radius: 12px;
        width: 90vw;
        max-width: 1200px;
        height: 80vh;
        display: flex;
        flex-direction: column;
        box-shadow: 0 25px 50px rgba(0, 0, 0, 0.3);
        border: 1px solid var(--border-default, #333);
    }

    .planner-header {
        display: flex;
        align-items: center;
        justify-content: space-between;
        padding: 16px 20px;
        border-bottom: 1px solid var(--border-default, #333);
        flex-shrink: 0;
    }

    .planner-title {
        display: flex;
        align-items: center;
        gap: 10px;
        font-weight: 600;
        font-size: 15px;
    }

    .planner-title-icon {
        font-size: 18px;
        color: var(--text-muted, #888);
    }

    .planner-date {
        color: var(--text-muted, #888);
        font-weight: 400;
    }

    .planner-close {
        background: none;
        border: none;
        color: var(--text-muted, #888);
        cursor: pointer;
        padding: 8px;
        border-radius: 6px;
        font-size: 18px;
    }

    .planner-close:hover {
        background: var(--bg-hover, #2a2a2a);
        color: var(--text-default, #e0e0e0);
    }

    .planner-kanban {
        display: flex;
        gap: 16px;
        padding: 20px;
        flex: 1;
        overflow: hidden;
    }

    .planner-column {
        flex: 1;
        display: flex;
        flex-direction: column;
        min-width: 0;
        background: var(--bg-subtle, #252525);
        border-radius: 8px;
        overflow: hidden;
    }

    .planner-column-header {
        display: flex;
        align-items: center;
        justify-content: space-between;
        padding: 12px 16px;
        border-bottom: 1px solid var(--border-default, #333);
        flex-shrink: 0;
    }

    .planner-column-title {
        display: flex;
        align-items: center;
        gap: 8px;
        font-weight: 600;
        font-size: 13px;
        text-transform: uppercase;
        letter-spacing: 0.5px;
    }

    .planner-column-title.plan { color: #10b981; }
    .planner-column-title.doing { color: #f59e0b; }
    .planner-column-title.next { color: #6366f1; }

    .planner-column-count {
        background: var(--bg-hover, #2a2a2a);
        padding: 2px 8px;
        border-radius: 10px;
        font-size: 12px;
        font-weight: 500;
    }

    .planner-column-body {
        flex: 1;
        overflow-y: auto;
        padding: 12px;
        display: flex;
        flex-direction: column;
        gap: 8px;
    }

    .planner-card {
        background: var(--bg-default, #1e1e1e);
        border: 1px solid var(--border-default, #333);
        border-radius: 6px;
        padding: 12px;
        cursor: pointer;
        transition: all 0.15s ease;
    }

    .planner-card:hover {
        border-color: var(--border-hover, #555);
        box-shadow: 0 2px 8px rgba(0, 0, 0, 0.1);
    }

    .planner-card.in-plan {
        opacity: 0.5;
        border-style: dashed;
    }

    .planner-card.done {
        opacity: 0.5;
        text-decoration: line-through;
    }

    .planner-card-header {
        display: flex;
        align-items: flex-start;
        gap: 10px;
    }

    .planner-card-checkbox {
        width: 16px;
        height: 16px;
        border: 2px solid var(--border-default, #333);
        border-radius: 4px;
        flex-shrink: 0;
        margin-top: 2px;
    }

    .planner-card.done .planner-card-checkbox {
        background: var(--text-muted, #888);
        border-color: var(--text-muted, #888);
    }

    .planner-card-title {
        flex: 1;
        font-size: 14px;
        line-height: 1.4;
        word-break: break-word;
    }

    .planner-card-meta {
        margin-top: 8px;
        font-size: 12px;
        color: var(--text-muted, #888);
        display: flex;
        align-items: center;
        gap: 8px;
    }

    .planner-link {
        color: color(display-p3 0.396 0.784 0.733);
    }

    .planner-empty {
        text-align: center;
        padding: 40px 20px;
        color: var(--text-muted, #888);
    }

    .planner-empty-icon {
        font-size: 32px;
        margin-bottom: 8px;
        opacity: 0.5;
    }

    .planner-footer {
        padding: 12px 20px;
        border-top: 1px solid var(--border-default, #333);
        display: flex;
        justify-content: center;
        gap: 24px;
        font-size: 13px;
        color: var(--text-muted, #888);
        flex-shrink: 0;
    }

    .planner-stat {
        display: flex;
        align-items: center;
        gap: 6px;
    }

    .planner-stat-value {
        font-weight: 600;
        color: var(--text-default, #e0e0e0);
    }
`;

const SyncHubPlanner = {
  overlay: null,
  cssInjected: false,

  /**
   * Initialize planner sidebar item
   */
  init(ui, data) {
    this.ui = ui;
    this.data = data;

    // Inject CSS once
    if (!this.cssInjected) {
      ui.injectCSS(PLANNER_CSS);
      this.cssInjected = true;
    }

    // Add sidebar item
    this.sidebarItem = ui.addSidebarItem({
      label: 'Planner',
      icon: 'ti-bow',
      tooltip: 'Daily planning kanban',
      onClick: () => this.show(),
    });

    // Expose API
    window.plannerHub = {
      show: () => this.show(),
      hide: () => this.hide(),
      addToToday: (text, issueGuid) => this.addToToday(text, issueGuid),
      getTodayTasks: () => this.getTodayTasks(),
      getIssues: (status) => this.getIssuesByStatus(status),
    };

    console.log('[PlannerHub] Initialized');
  },

  /**
   * Show the planner overlay
   */
  async show() {
    if (this.overlay) return;

    // Create overlay
    this.overlay = document.createElement('div');
    this.overlay.className = 'planner-overlay';
    this.overlay.addEventListener('click', (e) => {
      if (e.target === this.overlay) this.hide();
    });

    // Create panel
    const panel = document.createElement('div');
    panel.className = 'planner-panel';
    this.overlay.appendChild(panel);

    // Render content
    await this.render(panel);

    document.body.appendChild(this.overlay);
  },

  /**
   * Hide the planner overlay
   */
  hide() {
    if (this.overlay) {
      this.overlay.remove();
      this.overlay = null;
    }
  },

  /**
   * Render the planner panel
   */
  async render(panel) {
    const todayTasks = await this.getTodayTasks();
    const { doing, next } = await this.getAllIssues();

    const todayDate = new Date().toLocaleDateString('en-US', {
      weekday: 'long',
      month: 'long',
      day: 'numeric',
    });

    // GUIDs of issues already in today's plan
    const plannedIssueGuids = new Set(
      todayTasks.filter((t) => t.linkedIssueGuid).map((t) => t.linkedIssueGuid)
    );

    const doneTasks = todayTasks.filter((t) => t.status === 'done').length;

    panel.innerHTML = `
      <div class="planner-header">
        <div class="planner-title">
          <span class="ti ti-calendar-time planner-title-icon"></span>
          <span>Planner</span>
          <span class="planner-date">${todayDate}</span>
        </div>
        <button class="planner-close" data-action="close">
          <span class="ti ti-x"></span>
        </button>
      </div>

      <div class="planner-kanban">
        ${this.renderPlanColumn(todayTasks)}
        ${this.renderIssueColumn('doing', 'Doing', doing, 'ti-progress', plannedIssueGuids)}
        ${this.renderIssueColumn('next', 'Next', next, 'ti-list-check', plannedIssueGuids)}
      </div>

      <div class="planner-footer">
        <div class="planner-stat">
          <span class="planner-stat-value">${todayTasks.length}</span>
          <span>in plan</span>
        </div>
        <div class="planner-stat">
          <span class="planner-stat-value">${doneTasks}</span>
          <span>completed</span>
        </div>
        <div class="planner-stat">
          <span class="planner-stat-value">${doing.length + next.length}</span>
          <span>issues queued</span>
        </div>
      </div>
    `;

    // Wire up actions
    panel.addEventListener('click', async (e) => {
      const action = e.target.closest('[data-action]')?.dataset.action;
      const card = e.target.closest('.planner-card');

      if (action === 'close') {
        this.hide();
        return;
      }

      if (card && card.dataset.type === 'issue' && !card.classList.contains('in-plan')) {
        const issueGuid = card.dataset.guid;
        await this.addToToday(null, issueGuid);
        // Re-render
        await this.render(panel);
      }
    });
  },

  /**
   * Render TODAY'S PLAN column
   */
  renderPlanColumn(tasks) {
    const cardsHtml =
      tasks.length > 0
        ? tasks.map((task) => this.renderTaskCard(task)).join('')
        : `<div class="planner-empty">
             <div class="planner-empty-icon ti ti-checkbox"></div>
             <div>No tasks planned</div>
             <div style="margin-top:8px;font-size:12px">Click issues to add them</div>
           </div>`;

    return `
      <div class="planner-column">
        <div class="planner-column-header">
          <div class="planner-column-title plan">
            <span class="ti ti-calendar-check"></span>
            Today's Plan
          </div>
          <span class="planner-column-count">${tasks.length}</span>
        </div>
        <div class="planner-column-body" data-column="plan">
          ${cardsHtml}
        </div>
      </div>
    `;
  },

  /**
   * Render an issue column (DOING or NEXT)
   */
  renderIssueColumn(id, title, issues, icon, plannedIssueGuids) {
    // Sort: unplanned first, planned at the end
    const sortedIssues = [...issues].sort((a, b) => {
      const aPlanned = plannedIssueGuids.has(a.guid);
      const bPlanned = plannedIssueGuids.has(b.guid);
      if (aPlanned === bPlanned) return 0;
      return aPlanned ? 1 : -1;
    });

    const cardsHtml =
      sortedIssues.length > 0
        ? sortedIssues.map((issue) => this.renderIssueCard(issue, plannedIssueGuids.has(issue.guid))).join('')
        : `<div class="planner-empty">
             <div class="planner-empty-icon ti ti-git-branch"></div>
             <div>No ${title.toLowerCase()} issues</div>
           </div>`;

    return `
      <div class="planner-column">
        <div class="planner-column-header">
          <div class="planner-column-title ${id}">
            <span class="ti ${icon}"></span>
            ${title}
          </div>
          <span class="planner-column-count">${issues.length}</span>
        </div>
        <div class="planner-column-body" data-column="${id}">
          ${cardsHtml}
        </div>
      </div>
    `;
  },

  /**
   * Render a task card (for TODAY'S PLAN)
   */
  renderTaskCard(task) {
    let titleHtml = '';
    if (task.text) {
      titleHtml = this.escapeHtml(task.text);
    }
    if (task.linkedIssueTitle) {
      const linkedHtml = `<span class="planner-link">${this.escapeHtml(task.linkedIssueTitle)}</span>`;
      titleHtml += (titleHtml ? ' ' : '') + linkedHtml;
    }
    if (!titleHtml) {
      titleHtml = '<span style="opacity:0.5">(empty task)</span>';
    }

    const isDone = task.status === 'done';
    const cardClass = `planner-card ${isDone ? 'done' : ''}`;

    return `
      <div class="${cardClass}" data-guid="${task.guid}" data-type="task">
        <div class="planner-card-header">
          <div class="planner-card-checkbox"></div>
          <div class="planner-card-title">${titleHtml}</div>
        </div>
      </div>
    `;
  },

  /**
   * Render an issue card (for DOING/NEXT)
   */
  renderIssueCard(issue, isInPlan) {
    const cardClass = `planner-card ${isInPlan ? 'in-plan' : ''}`;

    return `
      <div class="${cardClass}" data-guid="${issue.guid}" data-type="issue">
        <div class="planner-card-header">
          <div class="planner-card-title">${this.escapeHtml(issue.title)}</div>
        </div>
        <div class="planner-card-meta">
          <span>${issue.source || 'Issue'}</span>
          ${isInPlan ? '<span>• In plan</span>' : ''}
        </div>
      </div>
    `;
  },

  escapeHtml(text) {
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
  },

  // ===========================================================================
  // Data Access
  // ===========================================================================

  /**
   * Get today's journal record
   */
  async getTodayJournal() {
    const collections = await this.data.getAllCollections();
    const journalCollection = collections.find((c) => c.getName() === 'Journal');
    if (!journalCollection) return null;

    const records = await journalCollection.getAllRecords();
    // Use local date, not UTC (toISOString returns UTC)
    const now = new Date();
    const today = `${now.getFullYear()}${String(now.getMonth() + 1).padStart(2, '0')}${String(now.getDate()).padStart(2, '0')}`;
    let journal = records.find((r) => r.guid.endsWith(today));

    // Fallback: Thymer uses prev day until ~3am
    if (!journal) {
      const yesterday = new Date();
      yesterday.setDate(yesterday.getDate() - 1);
      const yesterdayStr = `${yesterday.getFullYear()}${String(yesterday.getMonth() + 1).padStart(2, '0')}${String(yesterday.getDate()).padStart(2, '0')}`;
      journal = records.find((r) => r.guid.endsWith(yesterdayStr));
    }

    return journal;
  },

  /**
   * Get all tasks from today's journal
   */
  async getTodayTasks() {
    const journal = await this.getTodayJournal();
    if (!journal) return [];

    const tasks = [];
    const seenGuids = new Set();

    try {
      const items = await journal.getLineItems();

      for (const item of items) {
        if (item.type !== 'task') continue;
        if (seenGuids.has(item.guid)) continue;
        seenGuids.add(item.guid);

        const segments = item.segments || [];
        let text = '';
        let linkedIssueGuid = null;

        for (const seg of segments) {
          if (seg.type === 'text' && typeof seg.text === 'string') {
            text += seg.text;
          } else if (seg.type === 'ref' && seg.text?.guid) {
            linkedIssueGuid = seg.text.guid;
          }
        }

        // Resolve linked issue title
        let linkedIssueTitle = null;
        if (linkedIssueGuid) {
          const linkedRecord = this.data.getRecord(linkedIssueGuid);
          if (linkedRecord) {
            linkedIssueTitle = linkedRecord.getName();
          }
        }

        const trimmedText = text.trim();
        if (!trimmedText && !linkedIssueGuid) continue;

        // Check status
        const rawStatus = item.props?.done;
        const status = this.getTaskStatus(rawStatus);

        tasks.push({
          guid: item.guid,
          text: trimmedText,
          status,
          linkedIssueGuid,
          linkedIssueTitle,
        });
      }
    } catch (e) {
      console.error('[PlannerHub] Error reading tasks:', e);
    }

    return tasks;
  },

  getTaskStatus(rawDone) {
    if (rawDone === 8) return 'done';
    if (rawDone === 1) return 'in_progress';
    return 'todo';
  },

  /**
   * Get Issues collection
   */
  async getIssuesCollection() {
    const collections = await this.data.getAllCollections();
    return collections.find((c) => c.getName() === 'Issues');
  },

  /**
   * Get issues by status
   */
  async getIssuesByStatus(status) {
    const issuesCollection = await this.getIssuesCollection();
    if (!issuesCollection) return [];

    const records = await issuesCollection.getAllRecords();
    const issues = [];

    for (const record of records) {
      const recordStatus = record.choice('status')?.toLowerCase() || '';
      if (recordStatus === status.toLowerCase()) {
        issues.push({
          guid: record.guid,
          title: record.getName() || '(untitled)',
          source: record.choice('source') || '',
          url: record.text('url'),
        });
      }
    }

    return issues;
  },

  /**
   * Get all issues categorized by status
   */
  async getAllIssues() {
    const issuesCollection = await this.getIssuesCollection();
    if (!issuesCollection) return { doing: [], next: [] };

    const records = await issuesCollection.getAllRecords();
    const doing = [];
    const next = [];

    for (const record of records) {
      const status = record.choice('status')?.toLowerCase() || '';
      const issue = {
        guid: record.guid,
        title: record.getName() || '(untitled)',
        source: record.choice('source') || '',
        url: record.text('url'),
      };

      if (status === 'doing') {
        doing.push(issue);
      } else if (status === 'next') {
        next.push(issue);
      }
    }

    return { doing, next };
  },

  // ===========================================================================
  // Add to Today
  // ===========================================================================

  /**
   * Add a task to today's journal
   * Creates: - [ ] work on [[uuid]]
   */
  async addToToday(text, issueGuid = null) {
    const journal = await this.getTodayJournal();
    if (!journal) {
      console.error('[PlannerHub] No journal found for today');
      return false;
    }

    try {
      // Build segments
      const segments = [];

      if (issueGuid) {
        segments.push({ type: 'text', text: 'work on ' });
        segments.push({ type: 'ref', text: { guid: issueGuid } });
      } else if (text) {
        segments.push({ type: 'text', text: text });
      } else {
        console.error('[PlannerHub] No text or issueGuid provided');
        return false;
      }

      // Get existing items to find insertion point
      const items = await journal.getLineItems();
      const topLevel = items.filter((item) => item.parent_guid === journal.guid);
      const lastItem = topLevel.length > 0 ? topLevel[topLevel.length - 1] : null;

      // Create new task at end of journal (no PlannerHub section)
      const newItem = await journal.createLineItem(null, lastItem, 'task');
      if (newItem) {
        newItem.setSegments(segments);
        console.log('[PlannerHub] Added task:', segments);
        return true;
      } else {
        console.error('[PlannerHub] createLineItem returned null');
        return false;
      }
    } catch (e) {
      console.error('[PlannerHub] Error adding task:', e);
      return false;
    }
  },
};
