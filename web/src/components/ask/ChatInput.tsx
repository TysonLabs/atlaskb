import { useState, useRef, useEffect, useCallback } from "react";
import { Send, Square } from "lucide-react";
import { useChatContext } from "./ChatProvider";

export function ChatInput() {
  const { state, sendMessage, abortStream } = useChatContext();
  const [input, setInput] = useState("");
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  // Auto-focus on mount and after sending
  useEffect(() => {
    textareaRef.current?.focus();
  }, [state.isStreaming]);

  const handleSend = useCallback(async () => {
    const text = input.trim();
    if (!text || state.isStreaming || !state.activeSession) return;
    setInput("");
    // Reset textarea height
    if (textareaRef.current) {
      textareaRef.current.style.height = "auto";
    }
    await sendMessage(text);
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

  // Auto-resize textarea
  const handleInput = (e: React.ChangeEvent<HTMLTextAreaElement>) => {
    setInput(e.target.value);
    const ta = e.target;
    ta.style.height = "auto";
    ta.style.height = Math.min(ta.scrollHeight, 150) + "px";
  };

  const disabled = !state.activeSession;
  const usage = state.contextUsage;
  const usagePct = usage ? Math.round((usage.totalTokens / usage.contextWindow) * 100) : null;

  return (
    <div className="border-t border-edge bg-surface p-4">
      <div className="max-w-5xl mx-auto">
        {usagePct !== null && (
          <div className="flex items-center gap-2 mb-2 text-xs text-foreground-muted">
            <div className="flex-1 h-1.5 bg-edge rounded-full overflow-hidden">
              <div
                className={`h-full rounded-full transition-all ${
                  usagePct > 80 ? "bg-syn-red" : usagePct > 50 ? "bg-syn-yellow" : "bg-accent"
                }`}
                style={{ width: `${Math.min(usagePct, 100)}%` }}
              />
            </div>
            <span className="shrink-0 tabular-nums">
              {usagePct}% context ({usage!.totalTokens.toLocaleString()} / {usage!.contextWindow.toLocaleString()})
            </span>
          </div>
        )}
        <div className="flex items-end gap-3">
          <textarea
            ref={textareaRef}
            value={input}
            onChange={handleInput}
            onKeyDown={handleKeyDown}
            placeholder={disabled ? "Create or select a chat to start..." : "Ask about your codebase..."}
            disabled={disabled}
            rows={1}
            className="flex-1 resize-none px-4 py-3 border border-edge rounded-xl bg-surface-elevated text-foreground placeholder-foreground-muted focus:outline-none focus:ring-2 focus:ring-accent text-sm leading-relaxed disabled:opacity-50"
          />
          {state.isStreaming ? (
            <button
              onClick={abortStream}
              className="px-4 py-3 bg-syn-red text-surface rounded-xl hover:bg-syn-red/80 flex items-center gap-2 transition-colors shrink-0"
            >
              <Square size={16} />
            </button>
          ) : (
            <button
              onClick={handleSend}
              disabled={disabled || !input.trim()}
              className="px-4 py-3 bg-accent text-surface rounded-xl hover:bg-accent-hover disabled:opacity-50 flex items-center gap-2 transition-colors shrink-0"
            >
              <Send size={16} />
            </button>
          )}
        </div>
      </div>
    </div>
  );
}
