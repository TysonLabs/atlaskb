import { useRef, useEffect, useCallback, useState } from "react";
import { useChatContext } from "./ChatProvider";
import { ChatMessageBubble } from "./ChatMessageBubble";
import { ChatInput } from "./ChatInput";
import { EmptyState } from "./EmptyState";
import { TypingIndicator } from "./TypingIndicator";
import { Loader2 } from "lucide-react";

export function ChatArea() {
  const { state } = useChatContext();
  const scrollRef = useRef<HTMLDivElement>(null);
  const [isNearBottom, setIsNearBottom] = useState(true);

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

  // Auto-scroll on new content when near bottom
  useEffect(() => {
    if (isNearBottom) {
      scrollToBottom();
    }
  }, [state.streamingContent, state.activeSession?.messages.length, isNearBottom, scrollToBottom]);

  // Always scroll to bottom when switching sessions
  useEffect(() => {
    scrollToBottom();
    setIsNearBottom(true);
  }, [state.activeSession?.id, scrollToBottom]);

  if (state.isLoading) {
    return (
      <div className="flex-1 flex items-center justify-center">
        <Loader2 size={24} className="animate-spin text-foreground-muted" />
      </div>
    );
  }

  if (!state.activeSession || state.activeSession.messages.length === 0) {
    if (state.activeSession && state.isStreaming) {
      // Streaming first message — show it
    } else {
      return (
        <div className="flex-1 flex flex-col">
          <EmptyState />
          <ChatInput />
        </div>
      );
    }
  }

  const messages = state.activeSession?.messages || [];

  return (
    <div className="flex-1 flex flex-col min-h-0">
      <div
        ref={scrollRef}
        onScroll={handleScroll}
        className="flex-1 overflow-y-auto"
      >
        <div className="max-w-5xl mx-auto py-6 px-4 space-y-6">
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
            <div className="bg-syn-red/10 border border-syn-red/30 rounded-lg p-4 text-sm text-syn-red">
              {state.error}
            </div>
          )}
        </div>
      </div>

      <ChatInput />
    </div>
  );
}
