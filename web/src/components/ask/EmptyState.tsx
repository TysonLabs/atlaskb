import { MessageSquare } from "lucide-react";
import { useNavigate } from "react-router-dom";
import { useChatContext } from "./ChatProvider";

const exampleQuestions = [
  "What are the main architectural patterns used?",
  "How does authentication work?",
  "What key decisions have been made and why?",
  "What are the main dependencies and how are they used?",
];

export function EmptyState() {
  const { createSession, sendMessage, state } = useChatContext();
  const navigate = useNavigate();

  const handleExampleClick = async (question: string) => {
    if (state.activeSession) {
      await sendMessage(question);
    } else {
      const id = await createSession();
      navigate(`/ask/${id}`);
      // Small delay to let the session load, then send
      setTimeout(() => sendMessage(question), 100);
    }
  };

  return (
    <div className="flex-1 flex items-center justify-center">
      <div className="text-center max-w-md">
        <div className="w-16 h-16 rounded-2xl bg-accent/10 flex items-center justify-center mx-auto mb-6">
          <MessageSquare size={28} className="text-accent" />
        </div>
        <h2 className="text-xl font-semibold text-foreground mb-2">
          Ask anything about your codebase
        </h2>
        <p className="text-sm text-foreground-muted mb-8">
          AtlasKB will search your indexed knowledge base and synthesize an answer with evidence.
        </p>
        <div className="grid grid-cols-1 gap-2">
          {exampleQuestions.map((q) => (
            <button
              key={q}
              onClick={() => handleExampleClick(q)}
              className="text-left px-4 py-3 rounded-lg border border-edge hover:border-accent/40 hover:bg-accent/5 text-sm text-foreground-secondary hover:text-foreground transition-all"
            >
              {q}
            </button>
          ))}
        </div>
      </div>
    </div>
  );
}
