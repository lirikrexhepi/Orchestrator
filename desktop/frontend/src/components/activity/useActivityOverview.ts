import { useEffect, useState } from 'react';
import { ActivityOverview } from '../../../wailsjs/go/main/App';
import { activity } from '../../../wailsjs/go/models';

export type ActivityPeriod = 'week' | 'month' | 'year';

export function useActivityOverview(period: ActivityPeriod, project: string) {
  const [overview, setOverview] = useState<activity.Overview | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let live = true;
    ActivityOverview(period, project)
      .then((next) => {
        if (!live) return;
        setOverview(next);
        setError(null);
      })
      .catch((cause) => {
        if (live) setError(cause instanceof Error ? cause.message : String(cause));
      });
    return () => {
      live = false;
    };
  }, [period, project]);

  return { overview, error };
}
