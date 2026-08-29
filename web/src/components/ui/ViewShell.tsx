export function ViewShell({ title, sub, action, children, odId  }: any) {
  return (
    <section data-od-id={odId} data-testid={odId} className="od-scroll flex-1 overflow-y-auto bg-surface">
      <div className="mx-auto max-w-5xl px-8 py-8">
        <div className="mb-6 flex items-start justify-between gap-4">
          <div>
            <h1 className="text-[24px] font-semibold tracking-[-0.01em] text-fg">{title}</h1>
            {sub && <p className="mt-1 text-[13px] text-muted">{sub}</p>}
          </div>
          {action}
        </div>
        {children}
      </div>
    </section>
  );
}

