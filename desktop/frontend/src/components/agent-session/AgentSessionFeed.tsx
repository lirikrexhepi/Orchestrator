import React, { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { LegendList, type LegendListRef } from '@legendapp/list/react';
import { AgentStreamBlock } from './types';
import { UserChatBubble } from './UserChatBubble';
import { UserTurnMenu } from './UserTurnMenu';
import { ThinkingBlock } from './ThinkingBlock';
import { BashTool } from './BashTool';
import { SearchTool } from './SearchTool';
import { ToolGroup } from './ToolGroup';
import { EditTool } from './EditTool';
import { TodoTool } from './TodoTool';
import { PlanTool } from './PlanTool';
import { QuestionTool } from './QuestionTool';
import { ApprovalCard } from './ApprovalCard';
import { McpAppCard } from './McpAppCard';
import { MarkdownText } from './MarkdownText';
import { SubagentFeedRow } from './WorkspacePanel';
import { NoticeDivider } from './NoticeDivider';
import { MessageTimestamp } from './MessageTimestamp';
import { RespondToApproval, RespondToQuestion } from '../../../wailsjs/go/main/App';
import { buildFeedSegments, FeedSegment } from './feedTurns';
import { ChangesCard, WorkedFor, WorkingHeader } from './TurnParts';
import { PlainSegment, ROW_DOT_Y, TEXT_DOT_Y, TimelineAnimateContext, TimelineNode, TimelineSegment } from './Timeline';

const CARD_TYPES = new Set<AgentStreamBlock['type']>(['text', 'tool_question', 'approval_request', 'tool_plan', 'tool_todo', 'mcp_app', 'browser_screenshot']);

const isTimelineSegment = (segment: FeedSegment) =>
  segment.kind === 'run' ||
  segment.kind === 'worked' ||
  segment.kind === 'working' ||
  (segment.kind === 'block' && segment.block.type !== 'user' && segment.block.type !== 'notice');

const segmentDotY = (segment: FeedSegment) =>
  segment.kind === 'block' ? TEXT_DOT_Y : ROW_DOT_Y;

export interface VirtualFeedOptions {
  className?: string;
  style?: React.CSSProperties;
  itemClassName?: string;
  footer?: React.ReactNode;
  onScrollElement?: (element: HTMLElement | null) => void;
}

export interface AgentSessionFeedProps {
  blocks: AgentStreamBlock[];
  virtual?: VirtualFeedOptions;
  threadId?: string;
  className?: string;
  modelId?: string;
  driver?: string;
  onApprovePlan?: (blockId: string) => void;
  onAnswerQuestion?: (blockId: string, answers: string[]) => void;
  onSkipQuestion?: (blockId: string) => void;
  onDeleteTurn?: (turnId: string) => Promise<void> | void;
  onRespondApproval?: (requestId: string, decision: string) => void;
  onFileClick?: (path: string) => void;
  isActive?: boolean;
  isWorking?: boolean;
  onLoadEarlier?: () => void;
  onOpenSubagent?: (id: string) => void;
  hasEarlier?: boolean;
  loadingEarlier?: boolean;
}

type Handlers = Omit<AgentSessionFeedProps, 'blocks' | 'className' | 'isActive' | 'isWorking'>;

interface FeedBlockProps {
  block: AgentStreamBlock;
  isLatestText: boolean;
  isActive: boolean;
  defaultDriver?: string;
  defaultModel?: string;
  handlers: React.MutableRefObject<Handlers>;
}

const BrowserScreenshotPreview = React.memo(function BrowserScreenshotPreview({
  dataUrl,
  url,
  width,
  height,
}: {
  dataUrl: string;
  url?: string;
  width?: number;
  height?: number;
}) {
  const [expanded, setExpanded] = useState(false);
  return (
    <div className="flex flex-col items-start gap-1 py-0.5 select-none">
      <button
        type="button"
        onClick={() => setExpanded((v) => !v)}
        title={expanded ? 'Collapse screenshot' : 'Expand screenshot'}
        className="group relative overflow-hidden rounded-lg transition-all duration-150 cursor-pointer focus:outline-none"
      >
        <img
          src={dataUrl}
          alt={url ? `Browser screenshot of ${url}` : 'Browser screenshot'}
          draggable={false}
          className={`block rounded-lg object-contain transition-all duration-150 ${
            expanded ? 'max-w-full max-h-[520px]' : 'max-w-[260px] max-h-[170px] opacity-90 group-hover:opacity-100'
          }`}
        />
      </button>
      {(url || (width && height)) && (
        <span className="text-[11px] font-mono text-white/35 truncate max-w-full">
          {url || ''}
          {width && height ? `${url ? ' · ' : ''}${width}×${height}` : ''}
        </span>
      )}
    </div>
  );
});

/**
 * One feed entry. Memoised on the block object itself, so a streaming token
 * re-renders only the block it lands in, not the whole transcript. Callbacks
 * are read through a ref so their identity never defeats the memo.
 */
const FeedBlock = React.memo(function FeedBlock({
  block,
  isLatestText,
  isActive,
  defaultDriver,
  defaultModel,
  handlers,
}: FeedBlockProps) {
  const h = handlers.current;
  switch (block.type) {
    case 'user':
      return (
        <UserTurnMenu turnId={block.pending ? undefined : block.turnId} getDelete={() => handlers.current.onDeleteTurn}>
          <UserChatBubble message={block.content} timestamp={block.timestamp} files={block.files} />
        </UserTurnMenu>
      );

    case 'notice':
      return <NoticeDivider label={block.label} icon={block.icon} />;

    case 'text': {
      const tone = block.variant === 'error' ? 'text-rose-300' : 'text-current';
      const cursor = block.isStreaming && (
        <span className="inline-block w-1.5 h-3 bg-current ml-1 animate-pulse align-middle opacity-70" />
      );
      const blockDriver = block.driver || defaultDriver;
      const blockModel = block.model || defaultModel;
      const stamp = (
        <MessageTimestamp
          timestamp={block.timestamp}
          content={block.content}
          driver={blockDriver}
          model={blockModel}
          isStreaming={block.isStreaming}
        />
      );
      return (
        <div className={`text-[12.5px] font-normal font-(family-name:--app-font) ${tone} leading-relaxed pl-0.5 select-text`}>
          <MarkdownText content={block.content} />
          {cursor}
          {stamp}
        </div>
      );
    }

    case 'thinking':
      return (
        <ThinkingBlock
          isThinking={block.isThinking}
          thoughtText={block.thoughtText}
          durationSeconds={block.durationSeconds}
        />
      );

    case 'tool_group':
      return (
        <ToolGroup
          title={block.title}
          summary={block.summary}
          items={block.items}
          driver={block.driver || defaultDriver}
        />
      );

    case 'tool_bash':
      return (
        <BashTool
          command={block.command}
          summary={block.summary}
          output={block.output}
          status={block.status}
          exitCode={block.exitCode}
        />
      );

    case 'tool_search':
      return (
        <SearchTool
          files={block.files}
          query={block.query}
          summary={block.summary}
          isSearching={block.isSearching}
          onFileClick={(path) => handlers.current.onFileClick?.(path)}
        />
      );

    case 'tool_edit':
      return (
        <EditTool
          filePath={block.filePath}
          additions={block.additions}
          deletions={block.deletions}
          diffLines={block.diffLines}
          toolName={block.toolName}
          status={block.status}
        />
      );

    case 'tool_todo':
      return <TodoTool title={block.title} todos={block.todos} />;

    case 'tool_plan':
      return (
        <PlanTool
          planFile={block.planFile}
          title={block.title}
          summary={block.summary}
          approved={block.approved}
          blockId={block.id}
          onApproveBlock={(id) => handlers.current.onApprovePlan?.(id)}
        />
      );

    case 'tool_question':
      return (
        <QuestionTool
          questionNumber={block.questionNumber}
          totalQuestions={block.totalQuestions}
          question={block.question}
          options={block.options}
          items={block.items}
          answered={block.answered}
          selectedAnswer={block.selectedAnswer}
          blockId={block.id}
          onAnswerBlock={async (blockId, answers) => {
            const { onAnswerQuestion, threadId } = handlers.current;
            if (onAnswerQuestion) {
              onAnswerQuestion(blockId, answers);
            } else if (threadId) {
              try {
                await RespondToQuestion(threadId, blockId.replace(/^question-/, ''), answers);
              } catch (err) {
                console.error('Failed to respond to question:', err);
              }
            }
          }}
          onSkipBlock={async (blockId) => {
            const { onSkipQuestion, threadId } = handlers.current;
            if (onSkipQuestion) {
              onSkipQuestion(blockId);
            } else if (threadId) {
              try {
                await RespondToQuestion(threadId, blockId.replace(/^question-/, ''), []);
              } catch (err) {
                console.error('Failed to skip question:', err);
              }
            }
          }}
        />
      );

    case 'approval_request':
      return (
        <ApprovalCard
          requestID={block.requestID}
          title={block.title}
          detail={block.detail}
          options={block.options}
          status={block.status}
          decision={block.decision}
          onRespond={async (decision) => {
            const { onRespondApproval, threadId } = handlers.current;
            if (onRespondApproval) {
              await onRespondApproval(block.requestID, decision);
            } else if (threadId) {
              try {
                await RespondToApproval(threadId, block.requestID, decision);
              } catch (err) {
                console.error('Failed to respond to approval:', err);
                throw err;
              }
            }
          }}
        />
      );

    case 'subagent':
      return (
        <SubagentFeedRow
          agent={block}
          driver={block.driver || defaultDriver}
          modelId={block.model || defaultModel}
          onOpen={(agentId) => handlers.current.onOpenSubagent?.(agentId)}
        />
      );

    case 'mcp_app':
      return (
        <McpAppCard
          resourceUri={block.resourceUri}
          htmlContent={block.htmlContent}
          toolName={block.toolName}
          rawOutput={block.rawOutput}
          driver={block.driver || defaultDriver}
        />
      );

    case 'browser_screenshot':
      return (
        <BrowserScreenshotPreview
          dataUrl={block.dataUrl}
          url={block.url}
          width={block.width}
          height={block.height}
        />
      );

    default:
      return null;
  }
});

/**
 * AgentSessionFeed Component
 * Dynamic dispatcher that renders a stream of CLI / AI SDK events into their respective glass UI components.
 */
const AgentSessionFeedImpl: React.FC<AgentSessionFeedProps> = ({
  blocks,
  className = '',
  isActive = true,
  isWorking,
  modelId,
  driver,
  virtual,
  onLoadEarlier,
  hasEarlier,
  loadingEarlier,
  ...handlerProps
}) => {
  const handlers = React.useRef<Handlers>(handlerProps);
  handlers.current = handlerProps;

  const lastTextId = React.useMemo(() => {
    if (isWorking) return null;
    for (let i = blocks.length - 1; i >= 0; i--) {
      if (blocks[i].type === 'text') return blocks[i].id;
    }
    return null;
  }, [blocks, isWorking]);

  const segments = React.useMemo(
    () => buildFeedSegments(blocks, isWorking),
    [blocks, isWorking]
  );

  const renderBlock = (block: AgentStreamBlock) => {
    const node = (
      <FeedBlock
        key={block.id}
        block={block}
        isLatestText={block.id === lastTextId}
        isActive={isActive}
        defaultDriver={driver}
        defaultModel={modelId}
        handlers={handlers}
      />
    );
    return CARD_TYPES.has(block.type) ? <TimelineNode key={block.id}>{node}</TimelineNode> : node;
  };

  const renderSequence = (items: AgentStreamBlock[]) => {
    const out: React.ReactNode[] = [];
    let run: AgentStreamBlock[] = [];
    const flush = () => {
      if (run.length === 0) return;
      out.push(
        <div key={`run-${run[0].id}`} className="flex flex-col">
          {run.map(renderBlock)}
        </div>,
      );
      run = [];
    };
    for (const block of items) {
      if (block.type === 'text' || block.type === 'tool_question' || block.type === 'approval_request') {
        flush();
        out.push(renderBlock(block));
      } else {
        run.push(block);
      }
    }
    flush();
    return out;
  };

  const [drawLines, setDrawLines] = useState(false);
  useEffect(() => {
    const timer = window.setTimeout(() => setDrawLines(true), 900);
    return () => window.clearTimeout(timer);
  }, []);

  const renderSegment = (segment: FeedSegment) => {
        switch (segment.kind) {
          case 'block':
            return <React.Fragment key={segment.key}>{renderBlock(segment.block)}</React.Fragment>;
          case 'run':
            return (
              <div key={segment.key} className="flex flex-col">
                {segment.blocks.map(renderBlock)}
              </div>
            );
          case 'working':
            return (
              <TimelineNode key={segment.key} y={ROW_DOT_Y} tone="running">
                <WorkingHeader startedAt={segment.startedAt} />
              </TimelineNode>
            );
          case 'worked':
            return (
              <TimelineNode key={segment.key} y={ROW_DOT_Y}>
                <WorkedFor seconds={segment.seconds}>{renderSequence(segment.blocks)}</WorkedFor>
              </TimelineNode>
            );
          case 'changes':
            return <ChangesCard key={segment.key} edits={segment.edits} />;
          default:
            return null;
        }
  };

  const renderRow = (segment: FeedSegment, index: number, list: FeedSegment[], segmentClass: string) => {
    const body = <div className="w-full flex flex-col">{renderSegment(segment)}</div>;
    if (!isTimelineSegment(segment)) return (
        <PlainSegment key={segment.key} className={segmentClass}>
          {body}
        </PlainSegment>
      );
    return (
      <TimelineSegment
        key={segment.key}
        className={segmentClass}
        connectUp={index > 0 && isTimelineSegment(list[index - 1])}
        connectDown={index < list.length - 1 && isTimelineSegment(list[index + 1])}
        dotY={segmentDotY(segment)}
      >
        {body}
      </TimelineSegment>
    );
  };

  const listRef = useRef<LegendListRef | null>(null);
  const onScrollElement = virtual?.onScrollElement;
  useLayoutEffect(() => {
    if (!onScrollElement) return;
    onScrollElement(listRef.current?.getScrollableNode() ?? null);
    return () => onScrollElement(null);
  }, [onScrollElement]);

  const itemClassName = virtual?.itemClassName;
  const extraData = useMemo(
    () => ({ segments, lastTextId, drawLines, isActive, driver, modelId, itemClassName }),
    [segments, lastTextId, drawLines, isActive, driver, modelId, itemClassName],
  );

  if (virtual) {
    return (
      <TimelineAnimateContext.Provider value={drawLines}>
        <LegendList
          ref={listRef}
          className={virtual.className}
          style={virtual.style}
          data={segments}
          keyExtractor={(segment) => segment.key}
          renderItem={({ item, index }) => (
            <div className={virtual.itemClassName}>{renderRow(item, index, segments, 'w-full')}</div>
          )}
          extraData={extraData}
          estimatedItemSize={140}
          drawDistance={1200}
          recycleItems={false}
          initialScrollAtEnd
          maintainVisibleContentPosition={false}
          onStartReached={hasEarlier && onLoadEarlier ? () => onLoadEarlier() : undefined}
          onStartReachedThreshold={0.1}
          ListHeaderComponent={
            loadingEarlier ? (
              <div className={virtual.itemClassName}>
                <NoticeDivider label="Loading earlier messages" />
              </div>
            ) : hasEarlier && onLoadEarlier ? (
              <div className={`${virtual.itemClassName || ''} flex justify-center py-2`}>
                <button
                  type="button"
                  onClick={onLoadEarlier}
                  className="px-3 py-1 rounded-full text-[11px] font-medium font-(family-name:--app-font) bg-current/[0.06] hover:bg-current/[0.12] text-current/60 hover:text-current/90 transition-colors cursor-pointer"
                >
                  Load earlier messages
                </button>
              </div>
            ) : undefined
          }
          ListFooterComponent={
            <div className={virtual.itemClassName}>
              {virtual.footer}
              <div className="h-[200px] shrink-0 pointer-events-none" aria-hidden="true" />
            </div>
          }
        />
      </TimelineAnimateContext.Provider>
    );
  }

  return (
    <TimelineAnimateContext.Provider value={drawLines}>
    <div className={`flex flex-col ${className}`}>
      {segments.map((segment, index) => renderRow(segment, index, segments, 'feed-segment w-full'))}
      <div className="h-[200px] shrink-0 pointer-events-none" aria-hidden="true" />
    </div>
    </TimelineAnimateContext.Provider>
  );
};

export const AgentSessionFeed = React.memo(AgentSessionFeedImpl);
AgentSessionFeed.displayName = 'AgentSessionFeed';
