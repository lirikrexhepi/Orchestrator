import { MaterialIcon } from '../common/icons';
import React, { useState } from 'react';
import { BrowserOpenURL } from '../../../wailsjs/runtime/runtime';
import { BrowserTab } from './useBrowser';
import { CdpBrowserSurface } from './CdpBrowserSurface';

export interface BrowserFrameProps {
  tab: BrowserTab;
  onBlocked: (tabId: string) => void;
}

/**
 * The page surface rendered via headless Chromium CDP screencast.
 */
export const BrowserFrame: React.FC<BrowserFrameProps> = ({ tab, onBlocked }) => {
  const [isLoading, setLoading] = useState<boolean>(tab.url !== 'about:blank');
  const [browserError, setBrowserError] = useState<string | null>(null);

  if (!tab.url || tab.url === 'about:blank') {
    return (
      <div className="flex-1 min-h-0 rounded-[12px] bg-white/[0.03] border border-white/[0.07] grid place-items-center">
        <div className="flex flex-col items-center gap-2 px-6 text-center">
          <MaterialIcon name="globe" className="text-[26px] text-white/25"/>
          <span className="text-[12px] font-(family-name:--app-font) text-white/40 leading-relaxed">
            Enter an address, or pick a detected port above.
          </span>
        </div>
      </div>
    );
  }

  return (
    <div className="flex-1 min-h-0 relative rounded-[12px] overflow-hidden border border-white/[0.09] bg-[#121316]">
      <CdpBrowserSurface
        initialUrl={tab.url}
        externalNav={{ url: tab.url, nonce: tab.nonce }}
        onStateChange={(state) => {
          setLoading(state.loading);
        }}
        onError={(err) => {
          setBrowserError(err);
          onBlocked(tab.id);
        }}
        className="w-full h-full"
      />

      {isLoading && (
        <div className="absolute top-0 left-0 right-0 h-[2px] overflow-hidden bg-black/10 z-10">
          <div className="h-full w-1/3 bg-white/70 animate-[browser-progress_1.1s_ease-in-out_infinite]" />
        </div>
      )}

      {(browserError || tab.blocked) && (
        <div className="absolute inset-0 grid place-items-center bg-[#1b1e24] z-20">
          <div className="flex flex-col items-center gap-2.5 px-8 text-center max-w-[340px]">
            <MaterialIcon name="public_off" className="text-[26px] text-white/30"/>
            <span className="text-[12.5px] font-medium font-(family-name:--app-font) text-white/80 tracking-tight">
              Browser Surface Unavailable
            </span>
            <span className="text-[11.5px] font-(family-name:--app-font) text-white/45 leading-relaxed font-mono">
              {browserError || 'Embedding failed. You can view the page directly in your browser.'}
            </span>
            <button
              type="button"
              onClick={() => BrowserOpenURL(tab.url)}
              className="mt-1 h-[26px] px-3 rounded-[8px] bg-white/10 hover:bg-white/18 active:scale-95 text-[11.5px] font-medium font-(family-name:--app-font) text-white/80 hover:text-white transition-all duration-150 cursor-pointer"
            >
              Open in system browser
            </button>
          </div>
        </div>
      )}
    </div>
  );
};

export default BrowserFrame;
