import { render } from "@testing-library/react";
import { describe,expect,it } from "vitest";
import { Badge,PageHeader } from "./ui";
describe("dashboard primitives",()=>{it("renders status and hierarchy semantically",()=>{const{getByRole,getByText}=render(<><PageHeader eyebrow="System" title="Overview" description="Workspace status"/><Badge status="running"/></>);expect(getByRole("heading",{name:"Overview"})).toBeInTheDocument();expect(getByText("Running")).toHaveClass("status-running")})});
