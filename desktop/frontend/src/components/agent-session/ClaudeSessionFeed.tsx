import React from 'react';
import { AgentStreamBlock } from './types';
import { UserChatBubble } from './UserChatBubble';
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
import { NoticeDivider } from './NoticeDivider';
import { MessageTimestamp } from './MessageTimestamp';
import { RespondToApproval, RespondToQuestion } from '../../../wailsjs/go/main/App';

export interface ClaudeSessionFeedProps {
  blocks: AgentStreamBlock[];
  threadId?: string;
  modelId?: string;
  className?: string;
  onApprovePlan?: (blockId: string) => void;
  onAnswerQuestion?: (blockId: string, answers: string[]) => void;
  onSkipQuestion?: (blockId: string) => void;
  onRespondApproval?: (requestId: string, decision: string) => void;
  onFileClick?: (path: string) => void;
}

/**
 * ClaudeSessionFeed Component
 * Implements native Claude Code dialogue kinematics:
 * - Continuous conversational stream: commentary, diffs, and bash outputs flow naturally.
 * - Thinking blocks rendered inline as 'Thought for Xs >' accordions.
 * - Preserves full conversational history and interactive diff review.
 */
export const ClaudeSessionFeed: React.FC<ClaudeSessionFeedProps> = ({
  blocks,
  threadId,
  modelId,
  className = '',
  onApprovePlan,
  onAnswerQuestion,
  onSkipQuestion,
  onRespondApproval,
  onFileClick,
}) => {
  return (
    <div className={`flex flex-col gap-3.5 ${className}`}>
      {blocks.map((block) => {
        switch (block.type) {
          case 'user':
            return (
              <UserChatBubble
                key={block.id}
                message={block.content}
                timestamp={block.timestamp}
                files={block.files}
              />
            );

          case 'notice':
            return <NoticeDivider key={block.id} label={block.label} icon={block.icon} />;

          case 'text':
            return (
              <div
                key={block.id}
                className="text-[12.5px] font-normal font-(family-name:--app-font) text-white/90 leading-relaxed pl-0.5 select-text"
              >
                <MarkdownText content={block.content} />
                {block.isStreaming && (
                  <span className="inline-block w-1.5 h-3 bg-white/70 ml-1 animate-pulse align-middle" />
                )}
                <MessageTimestamp
                  timestamp={block.timestamp}
                  content={block.content}
                  driver="claude"
                  model={block.model || modelId}
                  isStreaming={block.isStreaming}
                />
              </div>
            );

          case 'thinking':
            return (
              <ThinkingBlock
                key={block.id}
                isThinking={block.isThinking}
                thoughtText={block.thoughtText}
                durationSeconds={block.durationSeconds}
              />
            );

          case 'tool_group':
            return (
              <ToolGroup
                key={block.id}
                title={block.title}
                summary={block.summary}
                items={block.items}
              />
            );

          case 'tool_bash':
            return (
              <BashTool
                key={block.id}
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
                key={block.id}
                files={block.files}
                query={block.query}
                summary={block.summary}
                isSearching={block.isSearching}
                onFileClick={onFileClick}
              />
            );

          case 'tool_edit':
            return (
              <EditTool
                key={block.id}
                filePath={block.filePath}
                additions={block.additions}
                deletions={block.deletions}
                diffLines={block.diffLines}
              />
            );

          case 'tool_todo':
            return <TodoTool key={block.id} title={block.title} todos={block.todos} />;

          case 'tool_plan':
            return (
              <PlanTool
                key={block.id}
                planFile={block.planFile}
                title={block.title}
                summary={block.summary}
                approved={block.approved}
                blockId={block.id}
                onApproveBlock={onApprovePlan}
              />
            );

          case 'tool_question':
            return (
              <QuestionTool
                key={block.id}
                questionNumber={block.questionNumber}
                totalQuestions={block.totalQuestions}
                question={block.question}
                options={block.options}
                items={block.items}
                answered={block.answered}
                selectedAnswer={block.selectedAnswer}
                blockId={block.id}
                onAnswerBlock={async (blockId, answers) => {
                  if (onAnswerQuestion) {
                    onAnswerQuestion(blockId, answers);
                  } else if (threadId) {
                    const qId = blockId.replace(/^question-/, '');
                    try {
                      await RespondToQuestion(threadId, qId, answers);
                    } catch (err) {
                      console.error('Failed to respond to question:', err);
                    }
                  }
                }}
                onSkipBlock={async (blockId) => {
                  if (onSkipQuestion) {
                    onSkipQuestion(blockId);
                  } else if (threadId) {
                    const qId = blockId.replace(/^question-/, '');
                    try {
                      await RespondToQuestion(threadId, qId, []);
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
                key={block.id}
                requestID={block.requestID}
                title={block.title}
                detail={block.detail}
                options={block.options}
                status={block.status}
                decision={block.decision}
                onRespond={async (decision) => {
                  if (onRespondApproval) {
                    onRespondApproval(block.requestID, decision);
                  } else if (threadId) {
                    try {
                      await RespondToApproval(threadId, block.requestID, decision);
                    } catch (err) {
                      console.error('Failed to respond to approval:', err);
                    }
                  }
                }}
              />
            );

          case 'mcp_app':
            return (
              <McpAppCard
                key={block.id}
                resourceUri={block.resourceUri}
                htmlContent={block.htmlContent}
                toolName={block.toolName}
                rawOutput={block.rawOutput}
                driver="claude"
              />
            );

          default:
            return null;
        }
      })}
    </div>
  );
};

export default ClaudeSessionFeed;
