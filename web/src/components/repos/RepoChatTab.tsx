import { useEffect, useRef, useState, useCallback } from "react";
import { ChatProvider, useChatContext } from "../ask/ChatProvider";
import { ChatMessageBubble } from "../ask/ChatMessageBubble";
import { TypingIndicator } from "../ask/TypingIndicator";
import { MessageSquare, Send, Square } from "lucide-react";

interface Props {
  repoId: string;
  repoName: string;
}

const exampleQuestions = [
  "What are the main architectural patterns used?",
  "How does error handling work?",
  "What key decisions have been made and why?",
  "What are the main dependencies?",
];

function RepoChatInner({ repoName }: { repoName: string }) {
  const { state, createSession, sendMessage, abortStream } = useChatContext();
  const [input, setInput] = useState("");
  const scrollRef = useRef<HTMLDivElement>(null);
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const [isNearBottom, setIsNearBottom] = useState(true);

  // Auto-create a session when the tab is opened
  useEffect(() => {
    if (!state.activeSession && !state.isLoading) {
      createSession();
    }
  }, [state.activeSession, state.isLoading, createSession]);

  const scrollToBottom = useCallback(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, []);

  const handleScroll = useCallback(() => {
    if (!scrollRef.current) return;
    const { scrollTop, scrollHeight, clientHeight } = scrollRef.current;
    setIsNearBottom(scrollHeight - scrollTop - clientHeight < 100);
  }, []);

  useEffect(() => {
    if (isNearBottom) scrollToBottom();
  }, [state.streamingContent, state.activeSession?.messages.length, isNearBottom, scrollToBottom]);

  const handleSend = useCallback(async (text?: string) => {
    const msg = (text || input).trim();
    if (!msg || state.isStreaming || !state.activeSession) return;
    setInput("");
    if (textareaRef.current) textareaRef.current.style.height = "auto";
    await sendMessage(msg);
  }, [input, state.isStreaming, state.activeSession, sendMessage]);

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      handleSend();
    }
    if (e.key === "Escape" && state.isStreaming) {
      abortStream();
    }
  };

  const handleInput = (e: React.ChangeEvent<HTMLTextAreaElement>) => {
    setInput(e.target.value);
    const ta = e.target;
    ta.style.height = "auto";
    ta.style.height = Math.min(ta.scrollHeight, 150) + "px";
  };

  if (!state.activeSession) {
    return (
      <div className="flex items-center justify-center py-12">
        <div className="text-center">
          <MessageSquare size={32} className="mx-auto text-foreground-muted mb-2" />
          <p className="text-sm text-foreground-secondary">Starting chat...</p>
        </div>
      </div>
    );
  }

  const messages = state.activeSession.messages;
  const isEmpty = messages.length === 0 && !state.isStreaming;

  return (
    <div className="bg-surface-elevated rounded-lg border border-edge overflow-hidden flex flex-col" style={{ height: 700 }}>
      <div className="px-4 py-2 border-b border-edge bg-surface-overlay/50">
        <p className="text-xs text-foreground-muted">
          Asking about <span className="font-medium text-foreground-secondary">{repoName}</span> — answers are scoped to this repo's knowledge base
        </p>
      </div>

      {/* Messages or empty state */}
      <div ref={scrollRef} onScroll={handleScroll} className="flex-1 overflow-y-auto min-h-0">
        {isEmpty ? (
          <div className="flex items-center justify-center h-full">
            <div className="text-center max-w-sm">
              <MessageSquare size={28} className="mx-auto text-accent mb-3" />
              <h3 className="text-sm font-medium text-foreground mb-1">Ask about {repoName}</h3>
              <p className="text-xs text-foreground-muted mb-4">
                Questions are answered using this repo's indexed knowledge base.
              </p>
              <div className="grid grid-cols-1 gap-1.5">
                {exampleQuestions.map((q) => (
                  <button
                    key={q}
                    onClick={() => handleSend(q)}
                    className="text-left px-3 py-2 rounded-lg border border-edge hover:border-accent/40 hover:bg-accent/5 text-xs text-foreground-secondary hover:text-foreground transition-all"
                  >
                    {q}
                  </button>
                ))}
              </div>
            </div>
          </div>
        ) : (
          <div className="max-w-5xl mx-auto py-4 px-4 space-y-4">
            {messages.map((msg) => (
              <ChatMessageBubble key={msg.id} message={msg} />
            ))}
            {state.isStreaming && (
              state.streamingContent ? (
                <ChatMessageBubble
                  message={{
                    id: "streaming",
                    role: "assistant",
                    content: "",
                    timestamp: new Date().toISOString(),
                  }}
                  isStreaming
                  streamingContent={state.streamingContent}
                  streamingEvidence={state.streamingEvidence}
                />
              ) : (
                <div className="flex justify-start">
                  <div className="flex items-start gap-3">
                    <div className="w-8 h-8 rounded-full bg-syn-magenta/20 flex items-center justify-center shrink-0" />
                    <TypingIndicator />
                  </div>
                </div>
              )
            )}
            {state.error && (
              <div className="bg-syn-red/10 border border-syn-red/30 rounded-lg p-3 text-sm text-syn-red">
                {state.error}
              </div>
            )}
          </div>
        )}
      </div>

      {/* Input */}
      <div className="border-t border-edge p-3">
        <div className="flex items-end gap-2">
          <textarea
            ref={textareaRef}
            value={input}
            onChange={handleInput}
            onKeyDown={handleKeyDown}
            placeholder={`Ask about ${repoName}...`}
            rows={1}
            className="flex-1 resize-none px-3 py-2 border border-edge rounded-lg bg-surface text-foreground placeholder-foreground-muted focus:outline-none focus:ring-1 focus:ring-accent text-sm leading-relaxed"
          />
          {state.isStreaming ? (
            <button
              onClick={abortStream}
              className="px-3 py-2 bg-syn-red text-surface rounded-lg hover:bg-syn-red/80 transition-colors shrink-0"
            >
              <Square size={16} />
            </button>
          ) : (
            <button
              onClick={() => handleSend()}
              disabled={!input.trim()}
              className="px-3 py-2 bg-accent text-surface rounded-lg hover:bg-accent-hover disabled:opacity-50 transition-colors shrink-0"
            >
              <Send size={16} />
            </button>
          )}
        </div>
      </div>
    </div>
  );
}

export function RepoChatTab({ repoId, repoName }: Props) {
  return (
    <ChatProvider repoId={repoId}>
      <RepoChatInner repoName={repoName} />
    </ChatProvider>
  );
}
