import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { Plus, Trash2, Pencil, Check, X, MessageSquare } from "lucide-react";
import { useChatContext } from "./ChatProvider";

export function ChatSidebar() {
  const { state, createSession, deleteSession, renameSession } = useChatContext();
  const navigate = useNavigate();
  const [editingId, setEditingId] = useState<string | null>(null);
  const [editTitle, setEditTitle] = useState("");

  const handleNewChat = async () => {
    const id = await createSession();
    navigate(`/ask/${id}`);
  };

  const handleSelect = (id: string) => {
    navigate(`/ask/${id}`);
  };

  const handleStartRename = (id: string, currentTitle: string) => {
    setEditingId(id);
    setEditTitle(currentTitle);
  };

  const handleConfirmRename = async () => {
    if (editingId && editTitle.trim()) {
      await renameSession(editingId, editTitle.trim());
    }
    setEditingId(null);
  };

  const handleCancelRename = () => {
    setEditingId(null);
  };

  const handleDelete = async (e: React.MouseEvent, id: string) => {
    e.stopPropagation();
    await deleteSession(id);
  };

  return (
    <div className="w-64 bg-sidebar border-r border-edge-subtle flex flex-col h-full shrink-0">
      <div className="p-3">
        <button
          data-new-chat
          onClick={handleNewChat}
          className="w-full flex items-center justify-center gap-2 px-3 py-2.5 bg-accent text-surface rounded-lg hover:bg-accent-hover transition-colors text-sm font-medium"
        >
          <Plus size={16} />
          New Chat
        </button>
      </div>

      <div className="flex-1 overflow-y-auto">
        {state.sessions.length === 0 ? (
          <div className="px-4 py-8 text-center text-foreground-muted text-xs">
            No conversations yet
          </div>
        ) : (
          <div className="py-1">
            {state.sessions.map((session) => {
              const isActive = state.activeSession?.id === session.id;
              const isEditing = editingId === session.id;

              return (
                <div
                  key={session.id}
                  onClick={() => !isEditing && handleSelect(session.id)}
                  className={`group flex items-center gap-2 px-3 py-2.5 mx-1.5 rounded-md cursor-pointer transition-colors ${
                    isActive
                      ? "bg-sidebar-active/15 border-l-2 border-accent"
                      : "hover:bg-sidebar-hover border-l-2 border-transparent"
                  }`}
                >
                  <MessageSquare size={14} className="shrink-0 text-foreground-muted" />

                  {isEditing ? (
                    <div className="flex-1 flex items-center gap-1">
                      <input
                        autoFocus
                        value={editTitle}
                        onChange={(e) => setEditTitle(e.target.value)}
                        onKeyDown={(e) => {
                          if (e.key === "Enter") handleConfirmRename();
                          if (e.key === "Escape") handleCancelRename();
                        }}
                        className="flex-1 bg-surface-elevated border border-edge rounded px-1.5 py-0.5 text-xs text-foreground outline-none"
                        onClick={(e) => e.stopPropagation()}
                      />
                      <button onClick={handleConfirmRename} className="text-syn-green hover:text-syn-green/80">
                        <Check size={12} />
                      </button>
                      <button onClick={handleCancelRename} className="text-syn-red hover:text-syn-red/80">
                        <X size={12} />
                      </button>
                    </div>
                  ) : (
                    <>
                      <span className="flex-1 text-xs text-foreground truncate">
                        {session.title}
                      </span>
                      <div className="hidden group-hover:flex items-center gap-0.5">
                        <button
                          onClick={(e) => {
                            e.stopPropagation();
                            handleStartRename(session.id, session.title);
                          }}
                          className="p-1 rounded hover:bg-surface-overlay text-foreground-muted hover:text-foreground"
                        >
                          <Pencil size={12} />
                        </button>
                        <button
                          onClick={(e) => handleDelete(e, session.id)}
                          className="p-1 rounded hover:bg-surface-overlay text-foreground-muted hover:text-syn-red"
                        >
                          <Trash2 size={12} />
                        </button>
                      </div>
                    </>
                  )}
                </div>
              );
            })}
          </div>
        )}
      </div>
    </div>
  );
}
