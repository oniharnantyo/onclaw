// @ts-nocheck
import { cx } from "../../lib/helpers";
import { STATUS } from "../../data/seed";

export function StatusDot({ status, live }) {
  const s = STATUS[status] || STATUS.idle;
  return <span className={cx('inline-block h-2 w-2 shrink-0 rounded-full', s.dot, live && s.live && 'od-live')} title={s.label}/>;
}

