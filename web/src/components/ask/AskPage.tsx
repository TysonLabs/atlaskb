import { useEffect } from "react";
import { useParams } from "react-router-dom";
import { ChatProvider, useChatContext } from "./ChatProvider";
import { ChatSidebar } from "./ChatSidebar";
import { ChatArea } from "./ChatArea";

function AskPageInner() {
  const { sessionId } = useParams<{ sessionId?: string }>();
  const { switchSession, clearActiveSession } = useChatContext();

  useEffect(() => {
    if (sessionId) {
      switchSession(sessionId);
    } else {
      clearActiveSession();
    }
  }, [sessionId, switchSession, clearActiveSession]);

  // Keyboard shortcuts
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      // Cmd/Ctrl+Shift+O — New chat (handled by sidebar, but also global)
      if ((e.metaKey || e.ctrlKey) && e.shiftKey && e.key === "o") {
        e.preventDefault();
        document.querySelector<HTMLButtonElement>("[data-new-chat]")?.click();
      }
    };
    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, []);

  return (
    <div className="flex h-full">
      <ChatSidebar />
      <div className="flex-1 flex flex-col min-w-0">
        <ChatArea />
      </div>
    </div>
  );
}

export function AskPage() {
  return (
    <ChatProvider>
      <AskPageInner />
    </ChatProvider>
  );
}
