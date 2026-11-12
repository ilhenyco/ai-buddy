import { useEffect, useId, useRef, type ReactNode } from 'react';
import { X, CircleNotch, ArrowUpRight, UserCircle } from '@phosphor-icons/react';

export function Avatar({ bot=false, size=40 }: {bot?:boolean;size?:number}) {
  return <span className={'avatar '+(bot?'avatar-bot':'')} style={{width:size,height:size}} aria-hidden="true">{bot?<img src="/assets/homych-team.png" alt=""/>:<UserCircle size={size*.7} weight="regular"/>}</span>;
}
export function Badge({ children, tone='gray' }: {children:ReactNode;tone?:string}) {return <span className={'badge badge-'+tone}>{children}</span>;}
export function Spinner({label='Загрузка'}:{label?:string}) {return <span className="loading" role="status"><CircleNotch size={19} className="spin"/>{label}</span>;}
export function ExternalLabel({children}:{children:ReactNode}) {return <span className="external-label">{children}<ArrowUpRight size={15}/></span>;}

export function Modal({title,children,onClose,wide=false,footer}:{title:string;children:ReactNode;onClose:()=>void;wide?:boolean;footer?:ReactNode}) {
  const ref=useRef<HTMLDivElement>(null); const titleId=useId(); const close=useRef(onClose); close.current=onClose;
  useEffect(()=>{
    const prior=document.activeElement as HTMLElement|null;
    ref.current?.querySelector<HTMLElement>('button, input, textarea, select, [tabindex="0"]')?.focus();
    const handler=(e:KeyboardEvent)=>{
      if(e.key==='Escape'){close.current();return;}
      if(e.key!=='Tab')return;
      const items=Array.from(ref.current?.querySelectorAll<HTMLElement>('button:not([disabled]),input:not([disabled]),textarea:not([disabled]),select:not([disabled]),a[href],[tabindex="0"]')??[]).filter(x=>x.offsetParent!==null);
      const first=items[0], last=items.at(-1);
      if(e.shiftKey&&document.activeElement===first){e.preventDefault();last?.focus();}
      if(!e.shiftKey&&document.activeElement===last){e.preventDefault();first?.focus();}
    };
    document.addEventListener('keydown',handler);
    return()=>{document.removeEventListener('keydown',handler);prior?.focus();};
  },[]);
  return <div className="modal-overlay" onMouseDown={e=>{if(e.target===e.currentTarget)onClose();}}><div ref={ref} role="dialog" aria-modal="true" aria-labelledby={titleId} className={'modal '+(wide?'modal-wide':'')}><header className="modal-header"><h2 id={titleId}>{title}</h2><button className="icon-button" aria-label="Закрыть окно" onClick={onClose}><X size={22}/></button></header><div className="modal-body">{children}</div>{footer&&<footer className="modal-footer">{footer}</footer>}</div></div>;
}
