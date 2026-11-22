import { MaterialIcon } from '../common/icons';
import React, { useEffect, useState } from 'react';

export interface QuestionOption {
  key: string;
  label: string;
  description?: string;
  isCustomInput?: boolean;
}

export interface QuestionItem {
  question: string;
  options: QuestionOption[];
  isMultiSelect?: boolean;
}

export interface QuestionToolProps {
  questionNumber?: number;
  totalQuestions?: number;
  question: string;
  options?: QuestionOption[];
  items?: QuestionItem[];
  isMultiSelect?: boolean;
  onAnswer?: (answers: string[]) => void;
  onSkip?: () => void;
  blockId?: string;
  onAnswerBlock?: (blockId: string, answers: string[]) => void;
  onSkipBlock?: (blockId: string) => void;
  answered?: boolean;
  selectedAnswer?: string;
  className?: string;
}

const QuestionToolImpl: React.FC<QuestionToolProps> = ({
  question,
  options = [],
  items,
  isMultiSelect,
  onAnswer,
  onSkip,
  blockId,
  onAnswerBlock,
  onSkipBlock,
  answered: initialAnswered = false,
  selectedAnswer: initialAnswer,
  className = '',
}) => {
  const list: QuestionItem[] = items && items.length > 0 ? items : [{ question, options, isMultiSelect }];
  const [page, setPage] = useState(0);
  const [picked, setPicked] = useState<Record<number, string>>({});
  const [multiPicked, setMultiPicked] = useState<Record<number, string[]>>({});
  const [customs, setCustoms] = useState<Record<number, string>>({});
  const [submitted, setSubmitted] = useState<string | null>(
    initialAnswered ? initialAnswer || 'Answered' : null,
  );

  useEffect(() => {
    if (initialAnswered) {
      setSubmitted(initialAnswer || 'Answered');
    } else {
      setSubmitted(null);
      setPicked({});
      setMultiPicked({});
      setCustoms({});
      setPage(0);
    }
  }, [initialAnswered, initialAnswer]);

  if (submitted) {
    return (
      <div className={`flex items-center gap-2 min-w-0 py-[3px] text-[12.5px] tracking-tight leading-[18px] font-(family-name:--app-font) text-current/55 ${className}`}>
        <span className="w-[15px] flex items-center justify-center shrink-0">
          <MaterialIcon name={submitted === 'Skipped' ? 'block' : 'check'} className="text-[14px]"/>
        </span>
        <span className="truncate min-w-0">
          {submitted === 'Skipped' ? 'Skipped question' : 'Answered'}
          {submitted !== 'Skipped' && submitted !== 'Answered' && <span className="text-current/40"> {submitted}</span>}
        </span>
      </div>
    );
  }

  const item = list[page];
  const isMulti = Boolean(item.isMultiSelect);
  const custom = item.options.find((option) => option.isCustomInput);
  const choices = item.options.filter((option) => !option.isCustomInput);
  const selected = picked[page];
  const selectedMulti = multiPicked[page] ?? [];
  const customText = customs[page] ?? '';
  const isLast = page === list.length - 1;

  const answerFor = (index: number): string => {
    const qItem = list[index];
    const typed = (customs[index] ?? '').trim();
    if (qItem.isMultiSelect) {
      const keys = multiPicked[index] ?? [];
      const labels = qItem.options
        .filter((candidate) => !candidate.isCustomInput && keys.includes(candidate.key))
        .map((candidate) => candidate.label);
      if (typed) labels.push(typed);
      return labels.join(', ');
    }
    const key = picked[index];
    const option = qItem.options.find((candidate) => candidate.key === key);
    if (option?.isCustomInput || (!option && typed)) return typed || option?.label || '';
    return option?.label || qItem.options.find((candidate) => !candidate.isCustomInput)?.label || '';
  };

  const canAdvance = isMulti
    ? selectedMulti.length > 0 || customText.trim().length > 0
    : Boolean(selected) || customText.trim().length > 0;

  const toggleMultiChoice = (key: string) => {
    setMultiPicked((prev) => {
      const current = prev[page] ?? [];
      const next = current.includes(key) ? current.filter((k) => k !== key) : [...current, key];
      return { ...prev, [page]: next };
    });
  };

  const advance = () => {
    if (!canAdvance) return;
    if (!isLast) {
      setPage(page + 1);
      return;
    }
    const answers = list.map((_, index) => answerFor(index));
    setSubmitted(answers.join(' · '));
    onAnswer?.(answers);
    if (blockId) onAnswerBlock?.(blockId, answers);
  };

  const skip = () => {
    setSubmitted('Skipped');
    onSkip?.();
    if (blockId) onSkipBlock?.(blockId);
  };

  return (
    <div
      className={`rounded-[16px] bg-current/[0.045] border border-current/[0.07] p-1.5 font-(family-name:--app-font) text-current select-none ${className}`}
    >
      <div className="flex items-start justify-between gap-3 px-2.5 pt-2 pb-2.5">
        <span className="text-[13px] font-medium tracking-tight leading-[1.45]">{item.question}</span>
        {list.length > 1 && (
          <span className="flex items-center gap-0.5 text-[11.5px] text-current/40 tabular-nums shrink-0 pt-px">
            <button
              type="button"
              disabled={page === 0}
              onClick={() => setPage(page - 1)}
              className="w-[18px] h-[18px] flex items-center justify-center rounded-[5px] enabled:hover:bg-current/[0.08] enabled:hover:text-current disabled:opacity-30 cursor-pointer disabled:cursor-default"
            >
              <MaterialIcon name="chevron_left" className="text-[14px]"/>
            </button>
            {page + 1} of {list.length}
            <button
              type="button"
              disabled={isLast || !canAdvance}
              onClick={() => setPage(page + 1)}
              className="w-[18px] h-[18px] flex items-center justify-center rounded-[5px] enabled:hover:bg-current/[0.08] enabled:hover:text-current disabled:opacity-30 cursor-pointer disabled:cursor-default"
            >
              <MaterialIcon name="chevron_right" className="text-[14px]"/>
            </button>
          </span>
        )}
      </div>

      <div className="flex flex-col gap-0.5">
        {choices.map((option, index) => {
          const active = isMulti ? selectedMulti.includes(option.key) : selected === option.key;
          return (
            <button
              key={option.key}
              type="button"
              role={isMulti ? 'checkbox' : undefined}
              aria-checked={isMulti ? active : undefined}
              onClick={() => {
                if (isMulti) {
                  toggleMultiChoice(option.key);
                } else {
                  setPicked((prev) => ({ ...prev, [page]: option.key }));
                }
              }}
              onDoubleClick={() => {
                if (isMulti) return;
                setPicked((prev) => ({ ...prev, [page]: option.key }));
                if (!isLast) setPage(page + 1);
              }}
              className={`flex items-start gap-3 px-2.5 py-2 rounded-[10px] text-left transition-colors duration-150 cursor-pointer ${
                active ? 'bg-current/[0.08]' : 'hover:bg-current/[0.045]'
              }`}
            >
              {isMulti ? (
                <span
                  className={`mt-[2px] w-3.5 h-3.5 rounded-[4px] border flex items-center justify-center shrink-0 transition-colors ${
                    active ? 'border-current/70 bg-current/20 text-current' : 'border-current/30 text-transparent'
                  }`}
                >
                  {active && <MaterialIcon name="check" className="text-[11px]" />}
                </span>
              ) : (
                <span className={`w-3 text-[12px] tabular-nums leading-[18px] shrink-0 ${active ? 'text-current/80' : 'text-current/35'}`}>
                  {index + 1}
                </span>
              )}
              <span className="text-[12.5px] tracking-tight leading-[18px] min-w-0">
                <span className={`font-medium ${active ? 'text-current' : 'text-current/85'}`}>{option.label}</span>
                {option.description && <span className="text-current/40"> {option.description}</span>}
              </span>
            </button>
          );
        })}

        <label
          className={`flex items-center gap-3 px-2.5 py-2 rounded-[10px] transition-colors duration-150 cursor-text ${
            !isMulti && custom && selected === custom.key ? 'bg-current/[0.08]' : 'hover:bg-current/[0.045]'
          }`}
        >
          <span className="w-3 text-[12px] text-current/35 leading-[18px] shrink-0">
            <MaterialIcon name="edit" className="text-[13px] align-[-2px]"/>
          </span>
          <input
            type="text"
            value={customText}
            onChange={(event) => {
              const value = event.target.value;
              setCustoms((prev) => ({ ...prev, [page]: value }));
              if (!isMulti) {
                setPicked((prev) => ({ ...prev, [page]: custom?.key ?? '' }));
              }
            }}
            onKeyDown={(event) => {
              if (event.key === 'Enter') advance();
            }}
            placeholder="Type your own answer"
            className="flex-1 min-w-0 bg-transparent border-0 outline-none p-0 text-[12.5px] tracking-tight leading-[18px] text-current placeholder:text-current/35 select-text"
          />
        </label>
      </div>

      <div className="flex items-center justify-end gap-1 px-1 pt-2 pb-0.5">
        <button
          type="button"
          onClick={skip}
          className="h-[28px] px-3 rounded-full text-[12px] text-current/45 hover:text-current transition-colors duration-150 cursor-pointer"
        >
          Skip
        </button>
        <button
          type="button"
          disabled={!canAdvance}
          onClick={advance}
          className="h-[28px] px-3.5 rounded-full bg-current/[0.12] enabled:hover:bg-current/[0.18] text-[12px] font-medium text-current disabled:text-current/35 disabled:bg-current/[0.05] transition-all duration-150 enabled:active:scale-95 cursor-pointer disabled:cursor-default"
        >
          {isLast ? 'Submit' : 'Next question'}
        </button>
      </div>
    </div>
  );
};

export const QuestionTool = React.memo(QuestionToolImpl);
QuestionTool.displayName = 'QuestionTool';

export default QuestionTool;
