import {it,expect} from "vitest";
import {render,screen,fireEvent} from "@testing-library/react";
import PortfolioDonut from "@/components/PortfolioDonut";
it("shows the sector name, value and share on pointer hover and keyboard focus",()=>{
 const {container}=render(<PortfolioDonut title="Portfolio" locale="en" currency="USD" slices={[{key:"a",name:"Bonds",value:300,color:"blue"},{key:"b",name:"Shares",value:100,color:"green"}]}/>);
 const first=container.querySelector('path')!;fireEvent.pointerEnter(first);expect(screen.getByRole("tooltip")).toHaveTextContent("Bonds");expect(screen.getByRole("tooltip")).toHaveTextContent("$300.00 · 75%");
 fireEvent.pointerLeave(screen.getByRole("img"));expect(screen.queryByRole("tooltip")).toBeNull();fireEvent.focus(first);expect(screen.getByRole("tooltip")).toHaveTextContent("Bonds");
});
