import { useCallback, useEffect, useState } from 'react';
import { EventsOn } from '../../../wailsjs/runtime/runtime';
import { RefreshUsage, UsageReport } from '../../../wailsjs/go/main/App';
import { session } from '../../../wailsjs/go/models';

const QUOTA_CHANNEL = 'usage:quota';
const HEARTBEAT_MS = 10_000;

export interface Usage {
  report: session.UsageReport | null;
  error: string | null;
}

/**
 * Keeps the quota report fresh while the panel is open.
 *
 * Opening the panel forces a fetch rather than accepting whatever is inside the
 * freshness window, which is what makes the figures current without a button to
 * press. That fetch runs in the background and announces itself when it lands,
 * so the repaint is driven by the event rather than by polling for it. The
 * heartbeat exists only to re-enter the Go side often enough that the freshness
 * window can expire and start the next fetch.
 */
export function useUsage(isOpen: boolean): Usage {
  const [report, setReport] = useState<session.UsageReport | null>(null);
  const [error, setError] = useState<string | null>(null);

  const read = useCallback(async (force: boolean) => {
    try {
      setReport(await (force ? RefreshUsage() : UsageReport()));
      setError(null);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    }
  }, []);

  // Always load cached usage once on mount and react to backend quota updates
  // without polling when the usage UI is closed.
  useEffect(() => {
    void read(false);
    return EventsOn(QUOTA_CHANNEL, () => void read(false));
  }, [read]);

  // Force-refresh and run the 10s heartbeat only while a usage panel is open.
  useEffect(() => {
    if (!isOpen) return;
    void read(true);
    const heartbeat = window.setInterval(() => void read(false), HEARTBEAT_MS);

    return () => {
      window.clearInterval(heartbeat);
    };
  }, [isOpen, read]);

  return { report, error };
}
