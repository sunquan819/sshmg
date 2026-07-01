
document.addEventListener('alpine:init', () => {
    Alpine.data('projectApp', () => {
        return {
            editConfigFile(comp) {
                this.editingConfigComponent = comp;
                this.configFileContent = '';
                this.showConfigModal = true;
            },
            saveConfigFile() {
                this.showToast('配置功能开发中', 'success');
                this.showConfigModal = false;
            }
        }
    })
});
