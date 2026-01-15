/**
 * Issues State Utilities
 * State ID patterns and label mappings.
 */
const IssuesState = {
  // State ID patterns - choice() returns ID not label
  OPEN_IDS: ['open', 'next', 'in_progress', 'inbox', 'doing'],
  CLOSED_IDS: ['closed', 'cancelled', 'done'],

  // Map labels to standard IDs
  LABEL_TO_ID: {
    Open: 'open',
    Next: 'next',
    'In Progress': 'in_progress',
    Inbox: 'inbox',
    Doing: 'doing',
    Closed: 'closed',
    Cancelled: 'cancelled',
    Done: 'done',
  },

  // Type mappings
  TYPE_LABELS: {
    issue: 'Issue',
    pull_request: 'PR',
    task: 'Task',
    bug: 'Bug',
    feature: 'Feature',
  },

  /**
   * Check if a state ID is an open state.
   */
  isOpen(stateId) {
    if (!stateId) return false;
    const id = stateId.toLowerCase();
    if (this.OPEN_IDS.includes(id)) return true;
    return id.includes('next') || id.includes('progress') || id.includes('doing');
  },

  /**
   * Convert label to ID for filtering.
   */
  labelToId(label) {
    return this.LABEL_TO_ID[label] || label.toLowerCase().replace(/ /g, '_');
  },

  /**
   * Check if record state matches target (handles both labels and IDs).
   */
  matches(record, targetLabel) {
    const stateId = record.prop('status')?.choice();
    if (!stateId) return false;
    const targetId = this.labelToId(targetLabel);
    if (stateId === targetId) return true;
    return stateId.toLowerCase().includes(targetId.toLowerCase());
  },

  /**
   * Convert ID back to label for display.
   */
  idToLabel(id, fieldType = 'state') {
    if (!id) return null;

    // Check state labels
    for (const [label, mappedId] of Object.entries(this.LABEL_TO_ID)) {
      if (mappedId === id || id.toLowerCase() === mappedId) return label;
    }

    // Check type labels
    if (this.TYPE_LABELS[id]) return this.TYPE_LABELS[id];

    // Fallback: capitalize and format
    return id.charAt(0).toUpperCase() + id.slice(1).replace(/_/g, ' ');
  },
};
