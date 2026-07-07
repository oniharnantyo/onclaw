import type { ContentBlock } from '../../types/chat';

export interface SourceLink {
  url: string;
  title: string;
}

export function extractSources(blocks?: ContentBlock[]): SourceLink[] {
  if (!blocks) return [];
  const sources: SourceLink[] = [];
  const seenUrls = new Set<string>();

  for (const block of blocks) {
    if (block.type === 'tool_result' && block.function_tool_result) {
      const result = block.function_tool_result;
      const name = result.name.toLowerCase();
      // Match typical research/fetching tools
      if (
        name.includes('search') ||
        name.includes('browser') ||
        name.includes('fetch') ||
        name.includes('web')
      ) {
        for (const contentBlock of result.content || []) {
          if (contentBlock.text?.text) {
            const text = contentBlock.text.text;

            // 1. Match markdown links: [Title](URL)
            const mdLinkRegex = /\[([^\]]+)\]\((https?:\/\/[^\s\)]+)\)/g;
            let match;
            while ((match = mdLinkRegex.exec(text)) !== null) {
              const title = match[1].trim();
              const url = match[2].trim();
              if (!seenUrls.has(url)) {
                seenUrls.add(url);
                sources.push({ url, title });
              }
            }

            // 2. Match raw URLs
            const rawUrlRegex = /(https?:\/\/[^\s\)"'<>]+)/g;
            let rawMatch;
            while ((rawMatch = rawUrlRegex.exec(text)) !== null) {
              const url = rawMatch[1].trim();
              if (!seenUrls.has(url)) {
                seenUrls.add(url);
                try {
                  const domain = new URL(url).hostname;
                  sources.push({ url, title: domain });
                } catch {
                  sources.push({ url, title: url });
                }
              }
            }
          }
        }
      }
    }
  }

  return sources;
}

export interface SourcesProps {
  blocks?: ContentBlock[];
}

export default function Sources({ blocks }: SourcesProps) {
  const sources = extractSources(blocks);

  if (sources.length === 0) return null;

  return (
    <div className="sources-container" role="region" aria-label="Derived sources">
      <span className="sources-label">Sources:</span>
      <div className="sources-list" role="list">
        {sources.map((src, idx) => {
          let hostname = '';
          try {
            hostname = new URL(src.url).hostname;
          } catch {
            // ignore invalid urls
          }

          const faviconUrl = hostname
            ? `https://www.google.com/s2/favicons?sz=32&domain=${hostname}`
            : '';

          return (
            <a
              key={idx}
              href={src.url}
              target="_blank"
              rel="noopener noreferrer"
              className="source-chip"
              role="listitem"
              title={src.url}
            >
              {faviconUrl && (
                <img
                  src={faviconUrl}
                  alt=""
                  className="source-favicon"
                  onError={(e) => {
                    // Hide broken icons
                    (e.target as HTMLImageElement).style.display = 'none';
                  }}
                />
              )}
              <span className="source-title">{src.title}</span>
            </a>
          );
        })}
      </div>
    </div>
  );
}
