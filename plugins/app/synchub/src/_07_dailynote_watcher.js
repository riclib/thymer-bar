/**
 * Daily Note Watcher
 *
 * Two-tier polling strategy:
 * 1. Fast poll (5s): Check last item's timestamp + task count
 * 2. Full check (60s): Full fingerprint scan as fallback
 * 3. Deep check (2s debounce): Build and send snapshot after edits stabilize
 *
 * Task status values (props.done):
 * - undefined/0: Unchecked
 * - 1: In Progress
 * - 2: Blocked/Waiting
 * - 8: Done/Completed
 */
const DailyNoteWatcher = {
  // Data API reference for resolving refs
  dataApi: null,

  // Status bar item reference
  statusBarItem: null,

  // Cached journal GUID (avoids search every poll)
  journalGuid: null,
  journalDate: null, // YYYYMMDD - invalidate cache on day change

  // Last known state for change detection
  lastFingerprint: null,      // Full fingerprint (count:maxTimestamp)
  lastTaskGuid: null,         // GUID of last task
  lastTaskTimestamp: null,    // Timestamp of last task
  lastTaskCount: 0,           // Number of tasks

  // Debounce state
  pendingDeepCheck: null,
  lastSeenMaxTimestamp: null,

  // Intervals
  fastPollInterval: null,
  fullCheckInterval: null,
  FAST_POLL_MS: 5000,   // 5 seconds - just check last item
  FULL_CHECK_MS: 60000, // 60 seconds - full fingerprint scan
  DEBOUNCE_MS: 2000,    // 2 seconds

  // Status names for wire format
  statusNames: {
    undefined: 'unchecked',
    0: 'unchecked',
    1: 'in-progress',
    2: 'blocked',
    3: 'cost',
    4: 'important',
    5: 'question',
    6: 'alert',
    7: 'starred',
    8: 'done',
  },

  /**
   * Initialize the watcher with status bar button and polling.
   * Uses global flag to prevent duplicate instances.
   */
  init(ui, data) {
    // Prevent duplicate instances (multiple VMs can load the plugin)
    if (window._dailyNoteWatcherActive) {
      return;
    }
    window._dailyNoteWatcherActive = true;

    this.dataApi = data;

    // Status bar button for manual check
    this.statusBarItem = ui.addStatusBarItem({
      htmlLabel: '<span class="ti ti-refresh synchub-watcher"></span>',
      tooltip: 'Send daily note snapshot',
      onClick: () => this.sendSnapshot(true),
    });

    // Start fast polling
    this.startPolling();

    console.log('[DailyNoteWatcher] Initialized');
  },

  /**
   * Start polling intervals.
   */
  startPolling() {
    // Initial snapshot after short delay
    setTimeout(() => this.sendSnapshot(false), 2000);

    // Fast poll every 5 seconds (just check last item)
    this.fastPollInterval = setInterval(() => this.fastCheck(), this.FAST_POLL_MS);

    // Full check every 60 seconds (full fingerprint scan)
    this.fullCheckInterval = setInterval(() => this.fullCheck(), this.FULL_CHECK_MS);
  },

  /**
   * Clean up on unload.
   */
  cleanup() {
    // Clear global flag so new instance can start
    window._dailyNoteWatcherActive = false;

    if (this.fastPollInterval) {
      clearInterval(this.fastPollInterval);
      this.fastPollInterval = null;
    }
    if (this.fullCheckInterval) {
      clearInterval(this.fullCheckInterval);
      this.fullCheckInterval = null;
    }
    if (this.pendingDeepCheck) {
      clearTimeout(this.pendingDeepCheck);
      this.pendingDeepCheck = null;
    }
    if (this.statusBarItem) {
      this.statusBarItem.remove();
      this.statusBarItem = null;
    }
  },

  /**
   * Fast check - only check the last task's timestamp (sub-ms).
   * Adding new items updates the previous last item's timestamp.
   */
  async fastCheck() {
    // Skip if we don't have a last task to check yet
    if (!this.lastTaskGuid) return;

    const journal = await this.getJournalCached();
    if (!journal) return;

    // Get line items and find our cached last task
    const lineItems = await journal.getLineItems();
    const tasks = (lineItems || []).filter(item => item.type === 'task');

    // Quick checks: count changed or last task missing
    if (tasks.length !== this.lastTaskCount) {
      this.scheduleDeepCheck(Date.now());
      return;
    }

    // Find the cached last task and check its timestamp
    const lastTask = tasks.find(t => t.guid === this.lastTaskGuid);
    if (!lastTask) {
      this.scheduleDeepCheck(Date.now());
      return;
    }

    const currentTimestamp = lastTask.getUpdatedAt?.()?.getTime() || 0;
    if (currentTimestamp !== this.lastTaskTimestamp) {
      this.scheduleDeepCheck(currentTimestamp);
      return;
    }

    // No change detected
  },

  /**
   * Full check - scan all tasks for changes (every 60s fallback).
   */
  async fullCheck() {
    const journal = await this.getJournalCached();
    if (!journal) return;

    const lineItems = await journal.getLineItems();
    const tasks = (lineItems || []).filter(item => item.type === 'task');

    // Build fingerprint: count + max timestamp
    let maxTimestamp = 0;
    for (const task of tasks) {
      const ts = task.getUpdatedAt?.()?.getTime() || 0;
      if (ts > maxTimestamp) maxTimestamp = ts;
    }
    const fingerprint = `${tasks.length}:${maxTimestamp}`;

    // Check if changed since last known
    if (this.lastFingerprint && fingerprint === this.lastFingerprint) {
      return; // No change
    }

    this.scheduleDeepCheck(maxTimestamp);
  },

  /**
   * Schedule a deep check after debounce period.
   */
  scheduleDeepCheck(maxTimestamp) {
    // Clear any pending check
    if (this.pendingDeepCheck) {
      clearTimeout(this.pendingDeepCheck);
    }

    // Remember the timestamp we saw
    this.lastSeenMaxTimestamp = maxTimestamp;

    // Schedule deep check
    this.pendingDeepCheck = setTimeout(() => this.deepCheck(), this.DEBOUNCE_MS);
  },

  /**
   * Deep check - verify timestamps stabilized, then send snapshot.
   */
  async deepCheck() {
    this.pendingDeepCheck = null;

    const journal = await this.getJournalCached();
    if (!journal) return;

    // Check current max timestamp
    const lineItems = await journal.getLineItems();
    const tasks = (lineItems || []).filter(item => item.type === 'task');
    let currentMaxTimestamp = 0;
    for (const task of tasks) {
      const ts = task.getUpdatedAt?.()?.getTime() || 0;
      if (ts > currentMaxTimestamp) currentMaxTimestamp = ts;
    }

    // If still changing, reschedule
    if (currentMaxTimestamp !== this.lastSeenMaxTimestamp) {
      this.scheduleDeepCheck(currentMaxTimestamp);
      return;
    }

    // Timestamp stabilized - send snapshot
    await this.sendSnapshot(false);
  },

  /**
   * Get journal with caching (avoids collection search every poll).
   */
  async getJournalCached() {
    // Use local date, not UTC (toISOString returns UTC)
    const now = new Date();
    const today = `${now.getFullYear()}${String(now.getMonth() + 1).padStart(2, '0')}${String(now.getDate()).padStart(2, '0')}`;

    // Invalidate cache on day change
    if (this.journalDate !== today) {
      this.journalGuid = null;
      this.journalDate = today;
    }

    // Use syncHub's getTodayJournal (it returns the record)
    const journal = await window.syncHub?.getTodayJournal();
    if (journal) {
      this.journalGuid = journal.guid;
    }
    return journal;
  },

  /**
   * Get today's journal - strict mode (no fallback to yesterday).
   * Returns { journal, reason } where reason explains why journal is null.
   */
  async getTodayJournalStrict() {
    if (!this.dataApi) {
      console.log('[DailyNoteWatcher] getTodayJournalStrict: no dataApi');
      return { journal: null, reason: 'no-data-api' };
    }

    const collections = await this.dataApi.getAllCollections();
    const journalCollection = collections.find((c) => c.getName() === 'Journal');
    if (!journalCollection) {
      console.log('[DailyNoteWatcher] getTodayJournalStrict: no Journal collection');
      return { journal: null, reason: 'no-journal-collection' };
    }

    // Use local date, not UTC (toISOString returns UTC)
    const now = new Date();
    const today = `${now.getFullYear()}${String(now.getMonth() + 1).padStart(2, '0')}${String(now.getDate()).padStart(2, '0')}`;
    const records = await journalCollection.getAllRecords();
    console.log('[DailyNoteWatcher] getTodayJournalStrict: looking for', today, 'in', records.length, 'records');

    // Debug: show last 5 GUIDs
    if (records.length > 0) {
      const guids = records.slice(-5).map(r => r.guid);
      console.log('[DailyNoteWatcher] last 5 journal GUIDs:', guids);
    }

    const journal = records.find((r) => r.guid.endsWith(today));

    if (!journal) {
      console.log('[DailyNoteWatcher] getTodayJournalStrict: NOT FOUND for', today);
      return { journal: null, reason: 'no-daily-note-for-today', date: today };
    }

    console.log('[DailyNoteWatcher] getTodayJournalStrict: FOUND', journal.guid);
    return { journal, reason: null };
  },

  /**
   * Build and send a full snapshot.
   * Uses strict date matching - won't send yesterday's data as today's.
   */
  async sendSnapshot(manual = false) {
    const result = await this.getTodayJournalStrict();
    if (!result.journal) {
      if (manual) {
        console.log('[DailyNoteWatcher] No daily note for today - please create it in Thymer');
      }
      // Send empty snapshot so thymer-bar knows we're connected but no data yet
      if (window.syncHub?.isConnected?.()) {
        window.syncHub.send({
          type: 'dailynote.snapshot',
          date: new Date().toISOString().slice(0, 10),
          observedAt: new Date().toISOString(),
          lines: [],
          reason: result.reason,
        });
      }
      return;
    }

    const journal = result.journal;
    const lineItems = await journal.getLineItems();
    if (!lineItems || lineItems.length === 0) return;

    // Filter for tasks
    const tasks = lineItems.filter(item => item.type === 'task');

    // Build snapshot (includes ref resolution)
    const lines = [];
    for (const task of tasks) {
      const { markdown, refs } = await this.extractMarkdownAndRefs(task);
      const status = task.props?.done;
      const statusName = this.statusNames[status] ?? String(status);
      const updatedAt = task.getUpdatedAt?.();

      // Skip empty tasks
      if (!markdown.trim()) continue;

      lines.push({
        guid: task.guid,
        markdown,
        refs: refs.length > 0 ? refs : undefined,
        status: statusName,
        updatedAt: updatedAt?.toISOString() || new Date().toISOString(),
      });
    }

    // Send to thymer-bar
    if (window.syncHub?.isConnected?.()) {
      const payload = {
        type: 'dailynote.snapshot',
        date: new Date().toISOString().slice(0, 10),
        observedAt: new Date().toISOString(),
        lines,
      };
      window.syncHub.send(payload);
      if (manual) {
        console.log(`[DailyNoteWatcher] Sent ${lines.length} tasks`);
      }
    }

    // Update state for change detection
    let maxTs = 0;
    for (const line of lines) {
      const ts = new Date(line.updatedAt).getTime();
      if (ts > maxTs) maxTs = ts;
    }
    this.lastFingerprint = `${lines.length}:${maxTs}`;
    this.lastTaskCount = lines.length;

    // Cache last task for fast polling
    if (lines.length > 0) {
      const lastLine = lines[lines.length - 1];
      this.lastTaskGuid = lastLine.guid;
      this.lastTaskTimestamp = new Date(lastLine.updatedAt).getTime();
    }
  },

  /**
   * Extract markdown text and resolve ref titles from task segments.
   */
  async extractMarkdownAndRefs(task) {
    const segments = task.segments || [];
    let text = '';
    const refs = [];

    for (const seg of segments) {
      switch (seg.type) {
        case 'text':
          text += seg.text;
          break;
        case 'link':
          text += `[${seg.text}](${seg.text})`;
          break;
        case 'datetime':
          if (seg.text?.d) {
            const d = seg.text.d;
            text += `[${d.slice(0,4)}-${d.slice(4,6)}-${d.slice(6,8)}]`;
          }
          break;
        case 'ref':
          const refGuid = seg.text?.guid;
          if (refGuid) {
            const title = await this.resolveRefTitle(refGuid);
            refs.push({ guid: refGuid, title: title || '(unknown)' });
            text += `[${title || '(unknown)'}](thymer:${refGuid})`;
          }
          break;
        default:
          text += seg.text || '';
      }
    }

    return { markdown: text.trim(), refs };
  },

  /**
   * Resolve a ref GUID to its title.
   */
  async resolveRefTitle(guid) {
    if (!this.dataApi) return null;

    try {
      const record = await SyncHubHelpers.findRecordByGUID(this.dataApi, guid);
      if (record) {
        return record.getName?.() || record.text?.('title') || null;
      }
    } catch (e) {
      // Silently fail
    }
    return null;
  },
};
