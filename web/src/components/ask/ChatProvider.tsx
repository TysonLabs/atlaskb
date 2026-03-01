import {
  createContext,
  useContext,
  useReducer,
  useCallback,
  useRef,
  useEffect,
  type ReactNode,
} from "react";
import { useNavigate } from "react-router-dom";
import { api } from "../../api/client";
import type { ChatSession, ChatSessionSummary, SearchResult } from "../../types";

interface ChatState {
  sessions: ChatSessionSummary[];
  activeSession: ChatSession | null;
  streamingContent: string;
  streamingEvidence: SearchResult[];
  isStreaming: boolean;
  isLoading: boolean;
  error: string | null;
}

type ChatAction =
  | { type: "SET_SESSIONS"; sessions: ChatSessionSummary[] }
  | { type: "SET_ACTIVE_SESSION"; session: ChatSession | null }
  | { type: "SET_LOADING"; loading: boolean }
  | { type: "SET_ERROR"; error: string | null }
  | { type: "START_STREAMING" }
  | { type: "APPEND_STREAMING_CONTENT"; text: string }
  | { type: "SET_STREAMING_EVIDENCE"; evidence: SearchResult[] }
  | { type: "STOP_STREAMING" }
  | { type: "ADD_USER_MESSAGE"; id: string; content: string; timestamp: string }
  | { type: "FINALIZE_ASSISTANT_MESSAGE"; id: string; content: string; evidence: SearchResult[]; timestamp: string };

const initialState: ChatState = {
  sessions: [],
  activeSession: null,
  streamingContent: "",
  streamingEvidence: [],
  isStreaming: false,
  isLoading: false,
  error: null,
};

function chatReducer(state: ChatState, action: ChatAction): ChatState {
  switch (action.type) {
    case "SET_SESSIONS":
      return { ...state, sessions: action.sessions };
    case "SET_ACTIVE_SESSION":
      return { ...state, activeSession: action.session, error: null };
    case "SET_LOADING":
      return { ...state, isLoading: action.loading };
    case "SET_ERROR":
      return { ...state, error: action.error };
    case "START_STREAMING":
      return { ...state, isStreaming: true, streamingContent: "", streamingEvidence: [], error: null };
    case "APPEND_STREAMING_CONTENT":
      return { ...state, streamingContent: state.streamingContent + action.text };
    case "SET_STREAMING_EVIDENCE":
      return { ...state, streamingEvidence: action.evidence };
    case "STOP_STREAMING":
      return { ...state, isStreaming: false };
    case "ADD_USER_MESSAGE": {
      if (!state.activeSession) return state;
      return {
        ...state,
        activeSession: {
          ...state.activeSession,
          messages: [
            ...state.activeSession.messages,
            { id: action.id, role: "user", content: action.content, timestamp: action.timestamp },
          ],
        },
      };
    }
    case "FINALIZE_ASSISTANT_MESSAGE": {
      if (!state.activeSession) return state;
      return {
        ...state,
        streamingContent: "",
        streamingEvidence: [],
        activeSession: {
          ...state.activeSession,
          messages: [
            ...state.activeSession.messages,
            {
              id: action.id,
              role: "assistant",
              content: action.content,
              evidence: action.evidence,
              timestamp: action.timestamp,
            },
          ],
        },
      };
    }
    default:
      return state;
  }
}

interface ChatContextValue {
  state: ChatState;
  loadSessions: () => Promise<void>;
  switchSession: (id: string) => Promise<void>;
  createSession: () => Promise<string>;
  deleteSession: (id: string) => Promise<void>;
  renameSession: (id: string, title: string) => Promise<void>;
  sendMessage: (question: string) => Promise<void>;
  abortStream: () => void;
  clearActiveSession: () => void;
}

const ChatContext = createContext<ChatContextValue | null>(null);

export function useChatContext() {
  const ctx = useContext(ChatContext);
  if (!ctx) throw new Error("useChatContext must be used within ChatProvider");
  return ctx;
}

export function ChatProvider({ children }: { children: ReactNode }) {
  const [state, dispatch] = useReducer(chatReducer, initialState);
  const abortControllerRef = useRef<AbortController | null>(null);
  const navigate = useNavigate();

  const loadSessions = useCallback(async () => {
    try {
      const sessions = await api.listChats();
      dispatch({ type: "SET_SESSIONS", sessions });
    } catch {
      // silently fail — sessions list isn't critical
    }
  }, []);

  const switchSession = useCallback(async (id: string) => {
    dispatch({ type: "SET_LOADING", loading: true });
    try {
      const session = await api.getChat(id);
      dispatch({ type: "SET_ACTIVE_SESSION", session });
    } catch {
      dispatch({ type: "SET_ERROR", error: "Failed to load chat session" });
    } finally {
      dispatch({ type: "SET_LOADING", loading: false });
    }
  }, []);

  const createSession = useCallback(async () => {
    const session = await api.createChat();
    dispatch({ type: "SET_ACTIVE_SESSION", session });
    await loadSessions();
    return session.id;
  }, [loadSessions]);

  const deleteSession = useCallback(async (id: string) => {
    await api.deleteChat(id);
    if (state.activeSession?.id === id) {
      dispatch({ type: "SET_ACTIVE_SESSION", session: null });
      navigate("/ask");
    }
    await loadSessions();
  }, [state.activeSession?.id, loadSessions, navigate]);

  const renameSession = useCallback(async (id: string, title: string) => {
    await api.updateChat(id, { title });
    if (state.activeSession?.id === id) {
      dispatch({
        type: "SET_ACTIVE_SESSION",
        session: { ...state.activeSession, title },
      });
    }
    await loadSessions();
  }, [state.activeSession, loadSessions]);

  const sendMessage = useCallback(async (question: string) => {
    if (!state.activeSession || state.isStreaming) return;

    const sessionId = state.activeSession.id;
    const msgId = crypto.randomUUID();
    dispatch({
      type: "ADD_USER_MESSAGE",
      id: msgId,
      content: question,
      timestamp: new Date().toISOString(),
    });
    dispatch({ type: "START_STREAMING" });

    const controller = new AbortController();
    abortControllerRef.current = controller;

    try {
      let fullContent = "";
      let evidence: SearchResult[] = [];

      for await (const event of api.chatMessage(sessionId, question, undefined, undefined, controller.signal)) {
        if (event.event === "facts") {
          try {
            evidence = JSON.parse(event.data);
            dispatch({ type: "SET_STREAMING_EVIDENCE", evidence });
          } catch { /* ignore */ }
        } else if (event.event === "chunk") {
          try {
            const text = JSON.parse(event.data);
            fullContent += text;
            dispatch({ type: "APPEND_STREAMING_CONTENT", text });
          } catch { /* ignore */ }
        } else if (event.event === "error") {
          dispatch({ type: "SET_ERROR", error: event.data });
        }
      }

      dispatch({
        type: "FINALIZE_ASSISTANT_MESSAGE",
        id: crypto.randomUUID(),
        content: fullContent,
        evidence,
        timestamp: new Date().toISOString(),
      });

      // Refresh sessions list to get updated title/timestamp
      await loadSessions();
    } catch (err) {
      if ((err as Error).name !== "AbortError") {
        dispatch({ type: "SET_ERROR", error: (err as Error).message || "Unknown error" });
      }
    } finally {
      dispatch({ type: "STOP_STREAMING" });
      abortControllerRef.current = null;
    }
  }, [state.activeSession, state.isStreaming, loadSessions]);

  const abortStream = useCallback(() => {
    abortControllerRef.current?.abort();
    dispatch({ type: "STOP_STREAMING" });
  }, []);

  const clearActiveSession = useCallback(() => {
    dispatch({ type: "SET_ACTIVE_SESSION", session: null });
  }, []);

  // Load sessions on mount
  useEffect(() => {
    loadSessions();
  }, [loadSessions]);

  return (
    <ChatContext.Provider
      value={{
        state,
        loadSessions,
        switchSession,
        createSession,
        deleteSession,
        renameSession,
        sendMessage,
        abortStream,
        clearActiveSession,
      }}
    >
      {children}
    </ChatContext.Provider>
  );
}
