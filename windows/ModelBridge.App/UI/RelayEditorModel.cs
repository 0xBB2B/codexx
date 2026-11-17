using System.ComponentModel;
using System.Runtime.CompilerServices;
using System.Threading.Tasks;
using ModelBridge.Services;

namespace ModelBridge.UI;

public sealed class RelayEditorModel : IRelayFields
{
    readonly AppModel model;
    string nameInput;
    string baseUrlInput;
    string apiKeyInput;
    string? nameError;
    string? baseUrlError;
    string? apiKeyError;
    DialogAlert? saveAlert;
    bool isSaving;

    internal RelayEditorModel(AppModel model, RelayConfig? config, string apiKey)
    {
        this.model = model;
        Id = config?.Id;
        nameInput = config?.Name ?? "";
        baseUrlInput = config?.BaseUrl ?? "";
        apiKeyInput = apiKey;
    }

    public event PropertyChangedEventHandler? PropertyChanged;

    internal string? Id { get; }

    public string Title => Id == null ? "添加配置" : "编辑配置";
    internal const string ApiKeyHelpText = "保存在系统凭据管理器中。";

    public string ApiKeyHelp => ApiKeyHelpText;
    public string SaveButtonText => isSaving ? "正在保存并获取模型…" : "保存";
    public bool FieldsEnabled => !isSaving;
    public bool CancelEnabled => !isSaving;

    public string NameInput
    {
        get => nameInput;
        set
        {
            if (Set(ref nameInput, value)) NameError = null;
        }
    }

    public string BaseUrlInput
    {
        get => baseUrlInput;
        set
        {
            if (Set(ref baseUrlInput, value)) BaseUrlError = null;
        }
    }

    public string ApiKeyInput
    {
        get => apiKeyInput;
        set
        {
            if (Set(ref apiKeyInput, value)) ApiKeyError = null;
        }
    }

    public string? NameError { get => nameError; internal set => Set(ref nameError, value); }
    public string? BaseUrlError { get => baseUrlError; internal set => Set(ref baseUrlError, value); }
    public string? ApiKeyError { get => apiKeyError; internal set => Set(ref apiKeyError, value); }
    public DialogAlert? SaveAlert { get => saveAlert; internal set => Set(ref saveAlert, value); }

    public bool IsSaving
    {
        get => isSaving;
        internal set
        {
            if (!Set(ref isSaving, value)) return;
            Raise(nameof(SaveButtonText));
            Raise(nameof(FieldsEnabled));
            Raise(nameof(CancelEnabled));
        }
    }

    public Task<bool> SaveAsync() => model.SaveRelayAsync(this);

    bool Set<T>(ref T field, T value, [CallerMemberName] string? name = null)
    {
        if (Equals(field, value)) return false;
        field = value;
        Raise(name);
        return true;
    }

    void Raise(string? name) => PropertyChanged?.Invoke(this, new PropertyChangedEventArgs(name));
}
