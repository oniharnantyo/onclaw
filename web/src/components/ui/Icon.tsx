import React from "react";

const ICONS = {
  search: [<circle key="a" cx="11" cy="11" r="7"/>, <path key="b" d="m21 21-4.35-4.35"/>],
  plus: [<path key="a" d="M12 5v14M5 12h14"/>],
  up: [<path key="a" d="M12 19V5M5 12l7-7 7 7"/>],
  bot: [<rect key="a" x="4" y="8" width="16" height="12" rx="3"/>, <path key="b" d="M12 8V5M9 13.5v2M15 13.5v2"/>, <circle key="c" cx="12" cy="3.5" r="1.5"/>],
  chat: [<path key="a" d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"/>],
  clock: [<circle key="a" cx="12" cy="12" r="9"/>, <path key="b" d="M12 7v5l3 2"/>],
  activity: [<path key="a" d="M22 12h-4l-3 8-6-16-3 8H2"/>],
  sliders: [<path key="a" d="M4 7h9M18 7h2M4 17h3M11 17h9"/>, <circle key="b" cx="15" cy="7" r="2"/>, <circle key="c" cx="8" cy="17" r="2"/>],
  users: [<path key="a" d="M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2"/>, <circle key="b" cx="9" cy="7" r="4"/>, <path key="c" d="M22 21v-2a4 4 0 0 0-3-3.87M16 3.13a4 4 0 0 1 0 7.75"/>],
  key: [<circle key="a" cx="8" cy="15" r="4"/>, <path key="b" d="m10.8 12.2 9-9M18 5l2 2M15 8l2 2"/>],
  link: [<path key="a" d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71"/>, <path key="b" d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71"/>],
  bell: [<path key="a" d="M18 8a6 6 0 0 0-12 0c0 7-3 9-3 9h18s-3-2-3-9"/>, <path key="b" d="M13.73 21a2 2 0 0 1-3.46 0"/>],
  clip: [<path key="a" d="m21.44 11.05-9.19 9.19a6 6 0 0 1-8.49-8.49l9.19-9.19a4 4 0 0 1 5.66 5.66l-9.2 9.19a2 2 0 0 1-2.83-2.83l8.49-8.48"/>],
  hash: [<path key="a" d="M4 9h16M4 15h16M10 3 8 21M16 3l-2 18"/>],
  chevdown: [<path key="a" d="m6 9 6 6 6-6"/>],
  chevright: [<path key="a" d="m9 6 6 6-6 6"/>],
  x: [<path key="a" d="M18 6 6 18M6 6l12 12"/>],
  check: [<path key="a" d="M20 6 9 17l-5-5"/>],
  play: [<path key="a" d="m7 4 13 8-13 8z"/>],
  dots: [<path key="a" d="M5 12h.01M12 12h.01M19 12h.01"/>],
  zap: [<path key="a" d="M13 2 3 14h9l-1 8 10-12h-9l1-8z"/>],
  globe: [<circle key="a" cx="12" cy="12" r="9"/>, <path key="b" d="M3 12h18M12 3a15 15 0 0 1 0 18 15 15 0 0 1 0-18z"/>],
  terminal: [<path key="a" d="m4 17 5-5-5-5M12 19h8"/>],
  db: [<ellipse key="a" cx="12" cy="5" rx="8" ry="3"/>, <path key="b" d="M4 5v14c0 1.66 3.58 3 8 3s8-1.34 8-3V5"/>, <path key="c" d="M4 12c0 1.66 3.58 3 8 3s8-1.34 8-3"/>],
  calendar: [<rect key="a" x="3" y="5" width="18" height="16" rx="2"/>, <path key="b" d="M8 3v4M16 3v4M3 11h18"/>],
  refresh: [<path key="a" d="M21 12a9 9 0 1 1-2.64-6.36M21 3v6h-6"/>],
  panel: [<rect key="a" x="3" y="4" width="18" height="16" rx="2"/>, <path key="b" d="M15 4v16"/>],
  pin: [<path key="a" d="M12 17v5M9 3h6l-1 8 4 3H6l4-3-1-8z"/>],
  shield: [<path key="a" d="M12 22s8-3 8-10V5l-8-3-8 3v7c0 7 8 10 8 10z"/>],
  help: [<circle key="a" cx="12" cy="12" r="9"/>, <path key="b" d="M9.1 9a3 3 0 0 1 5.8 1c0 2-3 3-3 3M12 17h.01"/>],
  file: [<path key="a" d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/>, <path key="b" d="M14 2v6h6M9 13h6M9 17h6"/>],
  copy: [<rect key="a" x="9" y="9" width="12" height="12" rx="2"/>, <path key="b" d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/>],
  eye: [<path key="a" d="M2 12s4-7 10-7 10 7 10 7-4 7-10 7S2 12 2 12z"/>, <circle key="b" cx="12" cy="12" r="3"/>],
  alert: [<path key="a" d="M10.29 3.86 1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/>, <path key="b" d="M12 9v4M12 17h.01"/>],
  at: [<circle key="a" cx="12" cy="12" r="4"/>, <path key="b" d="M16 8v5a3 3 0 0 0 6 0v-1a10 10 0 1 0-4 8"/>],
  edit: [<path key="a" d="M12 20h9"/>, <path key="b" d="M16.5 3.5a2.121 2.121 0 0 1 3 3L7 19l-4 1 1-4Z"/>],
  down: [<path key="a" d="M12 5v14M19 12l-7 7-7-7"/>],
  chevleft: [<path key="a" d="m15 18-6-6 6-6"/>],
  panelclose: [<rect key="a" x="3" y="3" width="18" height="18" rx="2"/>, <path key="b" d="M9 3v18"/>, <path key="c" d="m16 15-3-3 3-3"/>],
  panelopen: [<rect key="a" x="3" y="3" width="18" height="18" rx="2"/>, <path key="b" d="M9 3v18"/>, <path key="c" d="m14 9 3 3-3 3"/>],
  stop: [<rect key="a" x="7" y="7" width="10" height="10" rx="1.5" fill="currentColor" stroke="none"/>],
  plug: [<path key="a" d="M9 2v5M15 2v5"/>, <path key="b" d="M6 7h12v4a6 6 0 0 1-12 0z"/>, <path key="c" d="M12 17v5"/>],
  spark: [<path key="a" d="M12 3l3.2 5.8L21 12l-5.8 3.2L12 21l-3.2-5.8L3 12l5.8-3.2z"/>],
  logout: [<path key="a" d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4"/>, <polyline key="b" points="16 17 21 12 16 7"/>, <line key="c" x1="21" y1="12" x2="9" y2="12"/>]
};


export function Icon({ name, size = 16, className = '', sw = 1.8  }: any) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor"
      strokeWidth={sw} strokeLinecap="round" strokeLinejoin="round" className={className} aria-hidden="true">
      {ICONS[name]}
    </svg>
  );
}

