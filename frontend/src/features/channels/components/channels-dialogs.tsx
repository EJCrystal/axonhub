import { useEffect } from 'react';
import { useChannels } from '../context/channels-context';
import { useChannelDetails } from '../data/channels';
import { ChannelsActionDialog } from './channels-action-dialog';
import { ChannelsAPIKeyManagementDialog } from './channels-api-key-management-dialog';
import { ChannelsArchiveDialog } from './channels-archive-dialog';
import { ChannelsAvailabilityDialog } from './channels-availability-dialog';
import { ChannelsBulkApplyTemplateDialog } from './channels-bulk-apply-template-dialog';
import { ChannelsBulkArchiveDialog } from './channels-bulk-archive-dialog';
import { ChannelsBulkAutoDisableDialog } from './channels-bulk-auto-disable-dialog';
import { ChannelsBulkClearTemplateDialog } from './channels-bulk-clear-template-dialog';
import { ChannelsBulkManageTagsDialog } from './channels-bulk-manage-tags-dialog';
import { ChannelsBulkDeleteDialog } from './channels-bulk-delete-dialog';
import { ChannelsBulkDisableDialog } from './channels-bulk-disable-dialog';
import { ChannelsBulkEnableDialog } from './channels-bulk-enable-dialog';
import { ChannelsBulkImportDialog } from './channels-bulk-import-dialog';
import { ChannelsBulkOrderingDialog } from './channels-bulk-ordering-dialog';
import { ChannelsBulkTestDialog } from './channels-bulk-test-dialog';
import { ChannelsDeleteDialog } from './channels-delete-dialog';
import { ChannelsDisabledAPIKeysDialog } from './channels-disabled-api-keys-dialog';
import { ChannelsEndpointsDialog } from './channels-endpoints-dialog';
import { ChannelsErrorResolvedDialog } from './channels-error-resolved-dialog';
import { ChannelsModelMappingDialog } from './channels-model-mapping-dialog';
import { ChannelsModelPriceDialog } from './channels-model-price-dialog';
import { ChannelsOverrideDialog } from './channels-override-dialog';
import { ChannelsProxyDialog } from './channels-proxy-dialog';
import { ChannelsRateLimitDialog } from './channels-rate-limit-dialog';
import { ChannelsStatusDialog } from './channels-status-dialog';
import { ChannelsSystemSettingsDialog } from './channels-system-settings-dialog';
import { ChannelsTemplateManagerDialog } from './channels-template-manager-dialog';
import { ChannelsTestDialog } from './channels-test-dialog';
import { ChannelsTestHistoryDrawer } from './channels-test-history-drawer';
import { ChannelsTransformOptionsDialog } from './channels-transform-options-dialog';

export function ChannelsDialogs() {
  const { open, setOpen, currentRow: partialCurrentRow, setCurrentRow, selectedChannels } = useChannels();
  const detailsQuery = useChannelDetails(partialCurrentRow?.id, {
    enabled: Boolean(partialCurrentRow && open),
  });

  useEffect(() => {
    if (detailsQuery.data && partialCurrentRow?.id === detailsQuery.data.id && partialCurrentRow !== detailsQuery.data) {
      setCurrentRow(detailsQuery.data);
    }
  }, [detailsQuery.data, partialCurrentRow, setCurrentRow]);

  // List rows intentionally contain only fields required by visible columns.
  // Delay row-scoped dialogs until the full snapshot has been loaded so hiding
  // a column never removes data from edit/configuration dialogs.
  const currentRow =
    partialCurrentRow && (!open || detailsQuery.isError || detailsQuery.data === partialCurrentRow)
      ? (detailsQuery.data ?? partialCurrentRow)
      : null;
  return (
    <>
      <ChannelsSystemSettingsDialog />

      <ChannelsTemplateManagerDialog open={open === 'templates'} onOpenChange={(isOpen) => setOpen(isOpen ? 'templates' : null)} />

      <ChannelsActionDialog key='channel-add' open={open === 'add'} onOpenChange={(isOpen) => setOpen(isOpen ? 'add' : null)} />

      <ChannelsBulkArchiveDialog />

      <ChannelsBulkDisableDialog />

      <ChannelsBulkEnableDialog />

      <ChannelsBulkTestDialog />

      <ChannelsBulkDeleteDialog />

      <ChannelsBulkManageTagsDialog />

      <ChannelsBulkApplyTemplateDialog
        open={open === 'bulkApplyTemplate'}
        onOpenChange={(isOpen) => setOpen(isOpen ? 'bulkApplyTemplate' : null)}
        selectedChannels={selectedChannels}
      />

      <ChannelsBulkAutoDisableDialog
        open={open === 'bulkAutoDisable'}
        onOpenChange={(isOpen) => setOpen(isOpen ? 'bulkAutoDisable' : null)}
        selectedChannels={selectedChannels}
      />

      <ChannelsBulkClearTemplateDialog />

      <ChannelsBulkImportDialog isOpen={open === 'bulkImport'} onClose={() => setOpen(null)} />

      <ChannelsBulkOrderingDialog open={open === 'bulkOrdering'} onOpenChange={(isOpen) => setOpen(isOpen ? 'bulkOrdering' : null)} />

      {currentRow && (
        <>
          {open === 'edit' && (
            <ChannelsActionDialog
              key={`channel-edit-${currentRow.id}`}
              open={true}
              onOpenChange={(isOpen) => {
                if (isOpen) {
                  setOpen('edit');
                } else {
                  setOpen(null);
                  setTimeout(() => {
                    setCurrentRow(null);
                  }, 500);
                }
              }}
              currentRow={currentRow}
            />
          )}

          {open === 'duplicate' && (
            <ChannelsActionDialog
              key={`channel-duplicate-${currentRow.id}`}
              open={true}
              onOpenChange={(isOpen) => {
                if (isOpen) {
                  setOpen('duplicate');
                } else {
                  setOpen(null);
                  setTimeout(() => {
                    setCurrentRow(null);
                  }, 500);
                }
              }}
              duplicateFromRow={currentRow}
            />
          )}

          {open === 'viewModels' && (
            <ChannelsActionDialog
              key={`channel-view-models-${currentRow.id}`}
              open={true}
              onOpenChange={(isOpen) => {
                if (isOpen) {
                  setOpen('viewModels');
                } else {
                  setOpen(null);
                  setTimeout(() => {
                    setCurrentRow(null);
                  }, 500);
                }
              }}
              currentRow={currentRow}
              showModelsPanel={true}
            />
          )}

          {open === 'delete' && (
            <ChannelsDeleteDialog
              key={`channel-delete-${currentRow.id}`}
              open={true}
              onOpenChange={(isOpen) => {
                if (!isOpen) {
                  setOpen(null);
                  setTimeout(() => {
                    setCurrentRow(null);
                  }, 500);
                }
              }}
              currentRow={currentRow}
            />
          )}

          {open === 'modelMapping' && (
            <ChannelsModelMappingDialog
              key={`channel-model-mapping-${currentRow.id}`}
              open={true}
              onOpenChange={(isOpen) => {
                if (isOpen) {
                  setOpen('modelMapping');
                } else {
                  setOpen(null);
                  setTimeout(() => {
                    setCurrentRow(null);
                  }, 500);
                }
              }}
              currentRow={currentRow}
            />
          )}

          <ChannelsModelPriceDialog />

          {open === 'overrides' && (
            <ChannelsOverrideDialog
              key={`channel-overrides-${currentRow.id}`}
              open={true}
              onOpenChange={(isOpen) => {
                if (!isOpen) {
                  setOpen(null);
                  setTimeout(() => {
                    setCurrentRow(null);
                  }, 500);
                }
              }}
              currentRow={currentRow}
            />
          )}

          {open === 'proxy' && (
            <ChannelsProxyDialog
              key={`channel-proxy-${currentRow.id}`}
              open={true}
              onOpenChange={(isOpen) => {
                if (!isOpen) {
                  setOpen(null);
                  setTimeout(() => {
                    setCurrentRow(null);
                  }, 500);
                }
              }}
              currentRow={currentRow}
            />
          )}

          {open === 'status' && (
            <ChannelsStatusDialog
              key={`channel-status-${currentRow.id}`}
              open={true}
              onOpenChange={(isOpen) => {
                if (isOpen) {
                  setOpen('status');
                } else {
                  setOpen(null);
                  setTimeout(() => {
                    setCurrentRow(null);
                  }, 500);
                }
              }}
              currentRow={currentRow}
            />
          )}

          {open === 'archive' && (
            <ChannelsArchiveDialog
              key={`channel-archive-${currentRow.id}`}
              open={true}
              onOpenChange={(isOpen) => {
                if (isOpen) {
                  setOpen('archive');
                } else {
                  setOpen(null);
                  setTimeout(() => {
                    setCurrentRow(null);
                  }, 500);
                }
              }}
              currentRow={currentRow}
            />
          )}

          {open === 'test' && (
            <ChannelsTestDialog
              key={`channel-test-${currentRow.id}`}
              open={true}
              onOpenChange={(isOpen: boolean) => {
                if (isOpen) {
                  setOpen('test');
                } else {
                  setOpen(null);
                  setTimeout(() => {
                    setCurrentRow(null);
                  }, 500);
                }
              }}
              channel={currentRow}
            />
          )}

          {open === 'testHistory' && (
            <ChannelsTestHistoryDrawer
              key={`channel-test-history-${currentRow.id}`}
              open={true}
              onOpenChange={(isOpen) => {
                if (isOpen) {
                  setOpen('testHistory');
                } else {
                  setOpen(null);
                  setTimeout(() => {
                    setCurrentRow(null);
                  }, 500);
                }
              }}
              channel={currentRow}
            />
          )}

          {open === 'errorResolved' && (
            <ChannelsErrorResolvedDialog
              key={`channel-error-resolved-${currentRow.id}`}
              open={true}
              onOpenChange={(isOpen) => {
                if (!isOpen) {
                  setOpen(null);
                  setTimeout(() => {
                    setCurrentRow(null);
                  }, 500);
                }
              }}
            />
          )}

          {open === 'transformOptions' && (
            <ChannelsTransformOptionsDialog
              key={`channel-transform-options-${currentRow.id}`}
              open={true}
              onOpenChange={(isOpen) => {
                if (!isOpen) {
                  setOpen(null);
                  setTimeout(() => {
                    setCurrentRow(null);
                  }, 500);
                }
              }}
              currentRow={currentRow}
            />
          )}

          {open === 'rateLimit' && (
            <ChannelsRateLimitDialog
              key={`channel-rate-limit-${currentRow.id}`}
              open={true}
              onOpenChange={(isOpen) => {
                if (!isOpen) {
                  setOpen(null);
                  setTimeout(() => {
                    setCurrentRow(null);
                  }, 500);
                }
              }}
              currentRow={currentRow}
            />
          )}

          {open === 'endpoints' && (
            <ChannelsEndpointsDialog
              key={`channel-endpoints-${currentRow.id}`}
              open={true}
              onOpenChange={(isOpen) => {
                if (!isOpen) {
                  setOpen(null);
                  setTimeout(() => {
                    setCurrentRow(null);
                  }, 500);
                }
              }}
              channel={currentRow}
            />
          )}

          {open === 'disabledAPIKeys' && (
            <ChannelsDisabledAPIKeysDialog
              key={`channel-disabled-api-keys-${currentRow.id}`}
              open={true}
              onOpenChange={(isOpen) => {
                if (!isOpen) {
                  setOpen(null);
                  setTimeout(() => {
                    setCurrentRow(null);
                  }, 500);
                }
              }}
            />
          )}

          {open === 'availability' && (
            <ChannelsAvailabilityDialog
              key={`channel-availability-${currentRow.id}`}
              open={true}
              onOpenChange={(isOpen) => {
                if (!isOpen) {
                  setOpen(null);
                  setTimeout(() => {
                    setCurrentRow(null);
                  }, 500);
                }
              }}
              currentRow={currentRow}
            />
          )}

          {open === 'keyManagement' && (
            <ChannelsAPIKeyManagementDialog
              key={`channel-key-management-${currentRow.id}`}
              open={true}
              onOpenChange={(isOpen) => {
                if (!isOpen) {
                  setOpen(null);
                  setTimeout(() => {
                    setCurrentRow(null);
                  }, 500);
                }
              }}
            />
          )}
        </>
      )}
    </>
  );
}
