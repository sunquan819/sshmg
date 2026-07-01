
function addConfigFunctions() {
    const app = document.querySelector('[x-data="projectApp()"]');
    if (app && app.__x) {
        app.__x.$data.editConfigFile = function(comp) {
            this.editingConfigComponent = comp;
            this.configFileContent = '';
            this.showConfigModal = true;
        };
        app.__x.$data.saveConfigFile = function() {
            this.showToast('配置功能开发中', 'success');
            this.showConfigModal = false;
        };
    }
}

if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', addConfigFunctions);
} else {
    addConfigFunctions();
}
