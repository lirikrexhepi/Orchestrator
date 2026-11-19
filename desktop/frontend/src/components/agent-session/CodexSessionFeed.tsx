import React, { useState } from 'react';
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
import { McpAppCard } from './McpAppCard';
import { MarkdownText } from './MarkdownText';
import { NoticeDivider } from './NoticeDivider';
import { MessageTimestamp } from './MessageTimestamp';

export interface CodexSessionFeedProps {
  blocks: AgentStreamBlock[];
  modelId?: string;
  isWorking?: boolean;
  className?: string;
  onApprovePlan?: (blockId: string) => void;
  onAnswerQuestion?: (blockId: string, answers: string[]) => void;
  onSkipQuestion?: (blockId: string) => void;
  onFileClick?: (path: string) => void;
}

/**
 * CodexSessionFeed Component
 * Implements native OpenAI Codex / ChatGPT layout:
 * - Structured reasoning cards (o1/o3-mini style)
 * - Clean function and code execution outputs
 * - Markdown synthesized responses
 */
export const CodexSessionFeed: React.FC<CodexSessionFeedProps> = ({
  blocks,
  modelId,
  isWorking = false,
  className = '',
  onApprovePlan,
  onAnswerQuestion,
  onSkipQuestion,
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
                  driver="codex"
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
                items={(block as any).items}
                blockId={block.id}
                onAnswerBlock={onAnswerQuestion}
                onSkipBlock={onSkipQuestion}
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
                driver="codex"
              />
            );

          default:
            return null;
        }
      })}
    </div>
  );
};

export default CodexSessionFeed;
