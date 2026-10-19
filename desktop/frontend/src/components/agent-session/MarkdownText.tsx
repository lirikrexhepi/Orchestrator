import { MaterialIcon } from '../common/icons';
import React, { useState } from 'react';
import ReactMarkdown, { Components } from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { BrowserOpenURL } from '../../../wailsjs/runtime/runtime';

export interface MarkdownTextProps {
  content: string;
  className?: string;
}

/**
 * Renders agent output as markdown. Agents emit bold, inline code, lists and
 * fenced blocks constantly, so raw text reads as noise without this.
 *
 * Element renderers are declared once at module scope: react-markdown remounts
 * the tree whenever the components object identity changes, which would restart
 * every animation on each streamed token.
 */
/**
 * Fenced code block wrapper with a sleek, unobtrusive copy button.
 */
const PreBlock: React.FC<{ children?: React.ReactNode }> = ({ children }) => {
  const [copied, setCopied] = useState(false);

  const extractText = (node: any): string => {
    if (typeof node === 'string') return node;
    if (typeof node === 'number') return String(node);
    if (!node) return '';
    if (Array.isArray(node)) return node.map(extractText).join('');
    if (node.props?.children) return extractText(node.props.children);
    return '';
  };

  const handleCopy = (e: React.MouseEvent) => {
    e.stopPropagation();
    const text = extractText(children).replace(/\n$/, '');
    if (!text) return;
    navigator.clipboard.writeText(text);
    setCopied(true);
    setTimeout(() => setCopied(false), 1600);
  };

  return (
    <div className="relative group/code my-2.5">
      <pre className="px-3.5 py-2.5 pr-14 rounded-[10px] bg-black/35 border border-white/10 overflow-x-auto modern-scroll-area">
        {children}
      </pre>
      <button
        type="button"
        title={copied ? 'Copied to clipboard' : 'Copy command'}
        aria-label={copied ? 'Copied to clipboard' : 'Copy command'}
        onClick={handleCopy}
        className={`absolute top-2 right-2 h-[22px] px-1.5 rounded-[6px] flex items-center gap-1 transition-all duration-150 cursor-pointer select-none ${
          copied
            ? 'bg-emerald-500/25 text-emerald-300 border border-emerald-500/40 shadow-sm opacity-100'
            : 'bg-white/10 hover:bg-white/20 text-white/50 hover:text-white border border-white/10 opacity-0 group-hover/code:opacity-100 focus-visible:opacity-100 active:scale-90 shadow-sm'
        }`}
      >
        <MaterialIcon name={copied ? 'check' : 'content_copy'} className="text-[13px]"/>
        <span className="text-[10px] font-(family-name:--app-font) font-medium leading-none">
          {copied ? 'Copied' : 'Copy'}
        </span>
      </button>
    </div>
  );
};

const COMPONENTS: Components = {
  p: ({ children }) => <p className="my-1.5 leading-[1.65] opacity-90">{children}</p>,

  strong: ({ children }) => <strong className="font-semibold text-current opacity-100">{children}</strong>,
  em: ({ children }) => <em className="italic text-current opacity-95">{children}</em>,

  a: ({ children, href }) => (
    <a
      href={href}
      target="_blank"
      rel="noreferrer"
      onClick={(e) => {
        e.preventDefault();
        e.stopPropagation();
        if (href && /^(https?:|mailto:)/i.test(href)) void BrowserOpenURL(href);
      }}
      className="text-sky-400 hover:text-sky-300 underline underline-offset-2 decoration-current/25"
    >
      {children}
    </a>
  ),

  ul: ({ children }) => <ul className="my-2 pl-4 list-disc marker:text-current/35 space-y-1">{children}</ul>,
  ol: ({ children }) => <ol className="my-2 pl-4 list-decimal marker:text-current/35 space-y-1">{children}</ol>,
  li: ({ children }) => <li className="my-0.5 leading-[1.6] opacity-90">{children}</li>,

  h1: ({ children }) => <h1 className="text-[15px] font-semibold text-current mt-4 mb-2 tracking-tight leading-snug">{children}</h1>,
  h2: ({ children }) => <h2 className="text-[14px] font-semibold text-current mt-3.5 mb-1.5 tracking-tight leading-snug">{children}</h2>,
  h3: ({ children }) => <h3 className="text-[13px] font-semibold text-current mt-3 mb-1 tracking-tight leading-snug">{children}</h3>,
  h4: ({ children }) => <h4 className="text-[12.5px] font-semibold text-current/95 mt-2.5 mb-1 tracking-tight leading-snug">{children}</h4>,
  h5: ({ children }) => <h5 className="text-[12px] font-semibold text-current/90 mt-2 mb-0.5 tracking-tight">{children}</h5>,
  h6: ({ children }) => <h6 className="text-[11.5px] font-semibold uppercase tracking-wider text-current/75 mt-2 mb-0.5">{children}</h6>,

  blockquote: ({ children }) => (
    <blockquote className="my-2 pl-3 border-l-2 border-current/25 opacity-80 italic">{children}</blockquote>
  ),

  hr: () => <hr className="my-4 border-current/15" />,

  code: ({ className, children, ...props }) => {
    // Fenced blocks carry a language class; bare inline code does not.
    const isBlock = Boolean(className?.startsWith('language-'));
    if (!isBlock) {
      return (
        <code
          className="markdown-inline-code px-[5px] py-[1.5px] rounded-[5px] font-['Geist_Mono',monospace] text-[11.5px] tracking-tight font-normal box-decoration-clone"
          {...props}
        >
          {children}
        </code>
      );
    }
    return (
      <code className="block font-mono text-[11.5px] leading-relaxed text-current/90" {...props}>
        {children}
      </code>
    );
  },

  pre: ({ children }) => <PreBlock>{children}</PreBlock>,

  table: ({ children }) => (
    <div className="my-2 overflow-x-auto modern-scroll-area">
      <table className="w-full border-collapse text-[11.5px]">{children}</table>
    </div>
  ),
  th: ({ children }) => (
    <th className="border border-current/15 px-2.5 py-1.5 text-left font-semibold text-current">{children}</th>
  ),
  td: ({ children }) => <td className="border border-current/12 px-2.5 py-1.5 opacity-85">{children}</td>,
};

const PLUGINS = [remarkGfm];

const MarkdownTextImpl: React.FC<MarkdownTextProps> = ({ content, className = '' }) => (
  <div className={`markdown-body ${className}`}>
    <ReactMarkdown remarkPlugins={PLUGINS} components={COMPONENTS}>
      {content}
    </ReactMarkdown>
  </div>
);

export const MarkdownText = React.memo(MarkdownTextImpl);
MarkdownText.displayName = 'MarkdownText';

export default MarkdownText;
