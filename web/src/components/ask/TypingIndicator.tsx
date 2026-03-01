export function TypingIndicator() {
  return (
    <div className="flex items-center gap-1.5 px-4 py-3">
      <div className="typing-dot w-2 h-2 rounded-full bg-foreground-muted" style={{ animationDelay: "0ms" }} />
      <div className="typing-dot w-2 h-2 rounded-full bg-foreground-muted" style={{ animationDelay: "150ms" }} />
      <div className="typing-dot w-2 h-2 rounded-full bg-foreground-muted" style={{ animationDelay: "300ms" }} />
    </div>
  );
}
