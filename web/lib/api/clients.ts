// @ts-nocheck
import type { Transport } from "@connectrpc/connect";
import { createClient } from "@connectrpc/connect";

import { AuthService } from "../../proto/budget/v1/auth_pb";
import { UserService } from "../../proto/budget/v1/user_pb";
import { TenantService } from "../../proto/budget/v1/tenant_pb";
import { CategoryService } from "../../proto/budget/v1/category_pb";
import { TransactionService } from "../../proto/budget/v1/transaction_pb";
import { ReportService } from "../../proto/budget/v1/report_pb";
import { FxService } from "../../proto/budget/v1/fx_pb";
import { ImportService } from "../../proto/budget/v1/import_pb";

export function createClients(transport: Transport) {
  return {
    auth: createClient(AuthService as any, transport),
    user: createClient(UserService as any, transport),
    tenant: createClient(TenantService as any, transport),
    category: createClient(CategoryService as any, transport),
    transaction: createClient(TransactionService as any, transport),
    report: createClient(ReportService as any, transport),
    fx: createClient(FxService as any, transport),
    importSvc: createClient(ImportService as any, transport),
  } as const;
}


