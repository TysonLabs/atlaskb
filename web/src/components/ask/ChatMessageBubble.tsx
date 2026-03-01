import ReactMarkdown from "react-markdown";
import { User, Bot } from "lucide-react";
import type { ChatMessage, SearchResult } from "../../types";
import { EvidenceDrawer } from "./EvidenceDrawer";

interface ChatMessageBubbleProps {
  message: ChatMessage;
  isStreaming?: boolean;
  streamingContent?: string;
  streamingEvidence?: SearchResult[];
}

export function ChatMessageBubble({
  message,
  isStreaming,
  streamingContent,
  streamingEvidence,
}: ChatMessageBubbleProps) {
  const isUser = message.role === "user";
  const content = isStreaming ? streamingContent || "" : message.content;
  const evidence = isStreaming ? streamingEvidence : message.evidence;

  if (isUser) {
    return (
      <div className="flex justify-end animate-message-in">
        <div className="flex items-start gap-3 max-w-[70%]">
          <div className="bg-accent/10 border border-accent/20 rounded-2xl rounded-tr-sm px-4 py-3">
            <p className="text-sm text-foreground whitespace-pre-wrap">{message.content}</p>
          </div>
          <div className="w-8 h-8 rounded-full bg-accent/20 flex items-center justify-center shrink-0 mt-0.5">
            <User size={14} className="text-accent" />
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="flex justify-start animate-message-in">
      <div className="flex items-start gap-3 max-w-[85%]">
        <div className="w-8 h-8 rounded-full bg-syn-magenta/20 flex items-center justify-center shrink-0 mt-0.5">
          <Bot size={14} className="text-syn-magenta" />
        </div>
        <div className="flex-1 min-w-0">
          <div className="bg-surface-elevated border border-edge rounded-2xl rounded-tl-sm px-5 py-4">
            <div className="prose prose-sm max-w-none">
              <ReactMarkdown>{content || (isStreaming ? "" : "")}</ReactMarkdown>
            </div>
          </div>
          {evidence && evidence.length > 0 && (
            <div className="ml-1">
              <EvidenceDrawer evidence={evidence} />
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
