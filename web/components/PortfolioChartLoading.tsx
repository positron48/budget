export default function PortfolioChartLoading({ru}:{ru:boolean}){
 return <div role="progressbar" aria-label={ru?"Загрузка графика":"Loading chart"} aria-valuetext={ru?"Рассчитываем историю портфеля":"Calculating portfolio history"} className="flex h-80 items-center justify-center"><div className="w-2/3 space-y-4 motion-safe:animate-pulse"><div className="h-2 overflow-hidden rounded-full bg-slate-100 dark:bg-slate-700"><div className="h-full w-1/3 rounded-full bg-sky-400"/></div><p className="text-center text-sm text-slate-500">{ru?"Загружаем график…":"Loading chart…"}</p></div></div>;
}
