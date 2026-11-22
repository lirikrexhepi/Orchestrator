import { MaterialIcon } from '../common/icons';
import React, { useEffect, useState } from 'react';

export interface PlanToolProps {
  planFile?: string;
  title: string;
  summary: string;
  approved?: boolean;
  onApprove?: () => void;
  /** Identifier forwarded to onApproveBlock so the parent can pass a stable callback. */
  blockId?: string;
  onApproveBlock?: (blockId: string) => void;
  onReadDetailedPlan?: () => void;
  defaultExpanded?: boolean;
  className?: string;
}

/**
 * PlanTool Component
 * Displays agent plan overview with approval workflow and frosted white glass styling.
 */
const PlanToolImpl: React.FC<PlanToolProps> = ({
  planFile,
  title,
  summary,
  approved = false,
  onApprove,
  blockId,
  onApproveBlock,
  onReadDetailedPlan,
  defaultExpanded = false,
  className = '',
}) => {
  const [isApproved, setIsApproved] = useState(approved);
  const [isExpanded, setIsExpanded] = useState(defaultExpanded);

  useEffect(() => {
    setIsApproved(approved);
  }, [approved]);

  const handleApprove = () => {
    setIsApproved(true);
    onApprove?.();
    if (blockId) onApproveBlock?.(blockId);
  };

  return (
    <div
      className={`rounded-xl bg-current/[0.05] p-3 text-current max-w-full font-(family-name:--app-font) select-none flex flex-col gap-2 transition-all duration-150 border-0 shadow-none ${className}`}
    >
      <div
        onClick={() => setIsExpanded((prev) => !prev)}
        className="flex items-center justify-between cursor-pointer gap-2"
      >
        <div className="flex items-center gap-1.5 min-w-0">
          <MaterialIcon name="description" className="text-[15px] text-white/70 shrink-0"/>
          <span className="text-[11px] text-white/60 font-mono tracking-tight truncate leading-none">
            {planFile}
          </span>
        </div>

        <button
          type="button"
          className="w-[18px] h-[18px] rounded flex items-center justify-center text-white/60 hover:text-white shrink-0"
        >
          <MaterialIcon name="unfold_more" className={`text-[15px] transition-transform duration-200 ease-[cubic-bezier(0.16,1,0.3,1)] ${ isExpanded ? 'rotate-180' : 'rotate-0' }`}/>
        </button>
      </div>

      {/* Plan Content */}
      <div
        className="grid transition-[grid-template-rows,opacity] duration-200 ease-[cubic-bezier(0.16,1,0.3,1)]"
        style={{
          gridTemplateRows: isExpanded ? '1fr' : '0fr',
          opacity: isExpanded ? 1 : 0,
        }}
      >
        <div className="overflow-hidden">
          <div className="flex flex-col gap-2 pt-1">
            {/* Title */}
            <div className="text-[13px] font-semibold text-white tracking-tight">
              {title}
            </div>

            {/* Summary */}
            <div className="text-[12px] text-white/80 leading-relaxed tracking-tight select-text">
              {summary}
            </div>

            {/* Footer Action Controls */}
            <div className="flex items-center justify-between pt-1 mt-1">
              <button
                type="button"
                onClick={onReadDetailedPlan}
                className="text-[12px] text-white/70 hover:text-white underline underline-offset-2 transition-colors cursor-pointer"
              >
                Read detailed plan
              </button>

              {isApproved ? (
                <div className="flex items-center gap-1 text-[11px] font-medium text-emerald-300 bg-emerald-500/15 px-2.5 py-1 rounded-md">
                  <MaterialIcon name="check" className="text-[13px]"/>
                  Approved
                </div>
              ) : (
                <button
                  type="button"
                  onClick={handleApprove}
                  className="px-4 py-1.5 rounded-lg bg-blue-500 hover:bg-blue-600 active:scale-95 text-[12px] font-medium text-white transition-all cursor-pointer"
                >
                  Submit
                </button>
              )}
            </div>
          </div>
        </div>
      </div>
    </div>
  );
};

export const PlanTool = React.memo(PlanToolImpl);
PlanTool.displayName = 'PlanTool';

export default PlanTool;
