import { cx } from "../../lib/helpers";
import { STATUS } from "../../lib/constants";

export function StatusDot({ status, live  }: any) {
  const s = (STATUS as any)[status] || STATUS.idle;
  return <span className={cx('inline-block h-2 w-2 shrink-0 rounded-full', s.dot, live && s.live && 'od-live')} title={s.label}/>;
}

