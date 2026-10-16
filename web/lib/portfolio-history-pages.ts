import {shiftHistoryDate} from "./portfolio-history";

export const chartPageDays = 93;
// Keep a small window of read requests in flight, then consume in date order.
// Older servers can still return 31-day pages; they retain sequential loading.
// The caller verifies every page's scope, basis and exact daily continuity.
export async function* chartHistoryPages<T extends {nextDate: string; points: {date: string}[]}>(from: string, to: string, signal: AbortSignal, fetchPage: (date: string, signal: AbortSignal) => Promise<T>): AsyncGenerator<T> {
  const controller = new AbortController();
  const abort = () => controller.abort(signal.reason);
  signal.addEventListener("abort",abort,{once:true});
  if(signal.aborted)abort();
  // A tagged result handles rejection immediately even before its ordered turn.
  const pending = new Map<string, Promise<{page:T}|{error:unknown}>>();
  const request = (date:string) => {controller.signal.throwIfAborted();if(!pending.has(date))pending.set(date,fetchPage(date,controller.signal).then(page=>({page}),error=>({error})));};
  try {
    let cursor = from;
    request(cursor);
    for(let n=0;n<119;n++){
      signal.throwIfAborted();
      const result = await pending.get(cursor)!;pending.delete(cursor);
      if("error" in result)throw result.error;
      const page = result.page;
      if(page.nextDate===shiftHistoryDate(cursor,chartPageDays)&&page.points.length===chartPageDays){
        for(let i=1;i<=3;i++){const next=shiftHistoryDate(cursor,chartPageDays*i);if(next<=to)request(next);}
      }
      yield page;
      if(!page.nextDate)return;
      if(page.nextDate<=cursor||page.nextDate>to)throw new Error("invalid_history_cursor");
      cursor=page.nextDate;request(cursor);
    }
    throw new Error("history_limit");
  } finally {controller.abort();signal.removeEventListener("abort",abort);}
}
