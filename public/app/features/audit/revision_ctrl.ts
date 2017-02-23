///<reference path="../../headers/common.d.ts" />

import coreModule from 'app/core/core_module';

export class AuditLogCtrl {
  mode: any;
  dashboard: any;
  currentAuditLog: any;

  /** @ngInject */
  constructor(private $scope, private auditSrv) {
    $scope.ctrl = this;

    this.mode = 'list';
    this.dashboard = $scope.dashboard;
    this.reset();

    $scope.$watch('ctrl.mode', newVal => {
      if (newVal === 'compare') {
        this.compare();
      }
    });
  }

  auditLogChange() {
    return this.auditSrv.getAuditLog(this.dashboard).then(auditLog => {
      this.currentAuditLog = auditLog;
    });
  }

  reset() {
    this.auditLogChange();
  }

  compare() {
    console.log('compare', this.dashboard);
  }
}

coreModule.controller('AuditLogCtrl', AuditLogCtrl);
