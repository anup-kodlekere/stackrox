import static Services.checkForNoViolations
import static Services.waitForViolation

import io.stackrox.proto.storage.PolicyOuterClass
import io.stackrox.proto.storage.PolicyOuterClass.Policy
import io.stackrox.proto.storage.ScopeOuterClass
import io.stackrox.proto.storage.SignatureIntegrationOuterClass.CosignPublicKeyVerification
import io.stackrox.proto.storage.SignatureIntegrationOuterClass.SignatureIntegration

import objects.Deployment
import services.PolicyService
import services.SignatureIntegrationService

import spock.lang.Shared
import spock.lang.Tag
import spock.lang.Unroll

class ImageSignatureVerificationTest extends BaseSpecification {

    static final private String SIGNATURE_TESTING_NAMESPACE = "qa-signature-tests"

    // Names of the signature integration + policies that use the integration as the value of Trusted image signers.
    static final private String DISTROLESS = "Distroless"
    static final private String TEKTON = "Tekton"
    static final private String UNVERIFIABLE = "Unverifiable"
    static final private String DISTROLESS_AND_TEKTON = "Distroless+Tekton"
    static final private String POLICY_WITH_DISTROLESS_TEKTON_UNVERIFIABLE = "Distroless+Tekton+Unverifiable"
    static final private String SAME_DIGEST = "Same+Digest"

    // List of integration names used within tests.
    // NOTE: If you add a new name, make sure to add it here.
    static final private List<String> INTEGRATION_NAMES = [
            DISTROLESS,
            TEKTON,
            UNVERIFIABLE,
            DISTROLESS_AND_TEKTON,
            SAME_DIGEST,
    ]

    // Public keys used within signature integrations.
    static final private Map<String, String> DISTROLESS_PUBLIC_KEY = [
            // Source: https://vault.bitwarden.com/#/vault?itemId=95313e19-de46-4533-b160-af620120452a.
            "Distroless": """\
-----BEGIN PUBLIC KEY-----
MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAE2QA+RSRa8Vg99d/+SIXxDxN/yzB6
3sr+xlBGf1bF8Gdr9NPHPoOvbGY0AjgfQ2Zua2d7VxPCPKElLspit9GT8g==
-----END PUBLIC KEY-----""",
    ]
    static final private Map<String, String> TEKTON_COSIGN_PUBLIC_KEY = [
            // Source: https://vault.bitwarden.com/#/vault?itemId=95313e19-de46-4533-b160-af620120452a.
            "Tekton": """\
-----BEGIN PUBLIC KEY-----
MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEOp8ZlfXy8X2Xmnwd42tEQNQ89ywz
SA4GiM5faRXdIis6gk/codRMmN+fZh6E4uwYC3fgl6v4zqBYUZZUhqb2ow==
-----END PUBLIC KEY-----""",
    ]
    static final private Map<String, String> UNVERIFIABLE_COSIGN_PUBLIC_KEY = [
            // Manually created cosing public key via `cosign generate-key-pair`.
            "Unverifiable": """\
-----BEGIN PUBLIC KEY-----
MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEUpphKrUYSHvrR+r82Jn7Evg/d3L9
w9e2Azq1OYIh/pbeBMHARDrBaqqmuMR9+BfAaPAYdkNTU6f58M2zBbuL0A==
-----END PUBLIC KEY-----""",
    ]
    static final private Map<String, String> SAME_DIGEST_COSIGN_PUBLIC_KEY = [
            // Source: https://vault.bitwarden.com/#/vault?itemId=95313e19-de46-4533-b160-af620120452a.
            "Docker": """\
-----BEGIN PUBLIC KEY-----
MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEhsRRb4sl0Y4PeVSk9w/eYaWwigXj
QC+pUMTUP/ZmrvmKaA+pi55F+w3LqVJ17zwXKjaOEiEpn/+lntl/ieweeQ==
-----END PUBLIC KEY-----""",
    ]

    // Deployment holding an image which has a cosign signature that is verifiable with the DISTROLESS_PUBLIC_KEY.
    static final private Deployment DISTROLESS_DEPLOYMENT = new Deployment()
            .setName("with-signature-verified-by-distroless")
            // quay.io/rhacs-eng/qa-signatures:distroless-base
            .setImage("quay.io/rhacs-eng/qa-signatures@" +
                    "sha256:0e283722d5121a2610ce7fb85fd04800cc3a99fd2321f5678a2aa35f9e98d9c2")
            .addLabel("app", "image-with-signature-distroless-test")
            .setCommand(["sleep", "6000"])
            .setNamespace(SIGNATURE_TESTING_NAMESPACE)

    // Deployment holding an image which has a cosign signature that is verifiable with the TEKTON_PUBLIC_KEY.
    static final private Deployment TEKTON_DEPLOYMENT = new Deployment()
            .setName("with-signature-verified-by-tekton")
            // quay.io/rhacs-eng/qa-signatures:tekton
            .setImage("quay.io/rhacs-eng/qa-signatures@" +
                    "sha256:5bc15c838843506f6aaa6fa8d03b8d83f15b936a0362d6732afa0f45135fcf54")
            .addLabel("app", "image-with-signature-tekton-test")
            .setCommand(["/bin/sh", "-c", "/bin/sleep 600"])
            .setNamespace(SIGNATURE_TESTING_NAMESPACE)

    // Deployment holding an image which has a cosign signature that is not verifiable by any cosign public key.
    static final private Deployment UNVERIFIABLE_DEPLOYMENT = new Deployment()
            .setName("with-signature-unverifiable")
            // quay.io/rhacs-eng/qa-signatures:istio-proxy
            .setImage("quay.io/rhacs-eng/qa-signatures@" +
                    "sha256:134e99aa9597fdc17305592d13add95e2032609d23b4c508bd5ebd32ed2df47d")
            .addLabel("app", "image-with-unverifiable-signature-test")
            .setCommand(["/usr/local/bin/pilot-agent", "wait", "--timeoutSeconds", "6000"])
            .setNamespace(SIGNATURE_TESTING_NAMESPACE)

    // Deployment holding an image which does not have a cosign signature.
    static final private Deployment WITHOUT_SIGNATURE_DEPLOYMENT = new Deployment()
            .setName("without-signature")
            // quay.io/rhacs-eng/qa:nginx-204a9a8
            .setImage("quay.io/rhacs-eng/qa@" +
                    "sha256:7413e4ab770f308c01659dd1015e61dcc1dead3923d4347dbf3c59206594332f")
            .addLabel("app", "image-without-signature")
            .setNamespace(SIGNATURE_TESTING_NAMESPACE)

    // Deployment holding an image with the same digest as quay.io/rhacs-eng/qa-signatures:nginx that does
    // not have a cosign signature associated with it.
    static final private Deployment SAME_DIGEST_NO_SIGNATURE = new Deployment()
            .setName("same-digest-without-signature")
            // quay.io/rhacs-eng/qa:enforcement
            .setImage("quay.io/rhacs-eng/qa@" +
                    "sha256:3f13b4376446cf92b0cb9a5c46ba75d57c41f627c4edb8b635fa47386ea29e20")
            .addLabel("app", "image-same-digest-without-signature")
            .setNamespace(SIGNATURE_TESTING_NAMESPACE)

    // Deployment holding an image with the same digest as quay.io/rhacs-eng/qa:enforcement that does
    // have a cosign signature associated with it.
    static final private Deployment SAME_DIGEST_WITH_SIGNATURE = new Deployment()
            .setName("same-digest-with-signature")
            // quay.io/rhacs-eng/qa-signatures:nginx
            .setImage("quay.io/rhacs-eng/qa-signatures@" +
                    "sha256:3f13b4376446cf92b0cb9a5c46ba75d57c41f627c4edb8b635fa47386ea29e20")
            .addLabel("app", "image-same-digest-with-signature")
            .setNamespace(SIGNATURE_TESTING_NAMESPACE)

    // List of deployments used within the tests. This will be used during setup of the spec / teardown to create /
    // delete all deployments.
    // NOTE: If you add another deployment, make sure to add it here as well.
    static final private List<Deployment> DEPLOYMENTS = [
            DISTROLESS_DEPLOYMENT,
            TEKTON_DEPLOYMENT,
            UNVERIFIABLE_DEPLOYMENT,
            WITHOUT_SIGNATURE_DEPLOYMENT,
            SAME_DIGEST_NO_SIGNATURE,
            SAME_DIGEST_WITH_SIGNATURE,
    ]

    // Base policy which will be used for creating subsequent policies that have signature integration IDs as values.
    static final private Policy.Builder BASE_POLICY = Policy.newBuilder()
            .addLifecycleStages(PolicyOuterClass.LifecycleStage.DEPLOY)
            .addCategories("Test")
            .setDisabled(false)
            .setSeverityValue(2)
            .addAllScope([SIGNATURE_TESTING_NAMESPACE].collect
                    { ScopeOuterClass.Scope.newBuilder().setNamespace(it).build() })

    @Shared
    static final private List<String> CREATED_POLICY_IDS = []

    @Shared
    static final private Map<String, String> CREATED_SIGNATURE_INTEGRATIONS = [:]

    def setupSpec() {
        orchestrator.createNamespace(SIGNATURE_TESTING_NAMESPACE)
        addStackroxImagePullSecret(SIGNATURE_TESTING_NAMESPACE)

        // Signature integration "Distroless" which holds only the distroless cosign public key.
        String distrolessSignatureIntegrationID = createSignatureIntegration(
                DISTROLESS, DISTROLESS_PUBLIC_KEY
        )
        assert distrolessSignatureIntegrationID
        CREATED_SIGNATURE_INTEGRATIONS.put(DISTROLESS, distrolessSignatureIntegrationID)

        // Signature integration "Tekton" which holds only the tekton cosign public key.
        String tektonSignatureIntegrationID = createSignatureIntegration(
                TEKTON, TEKTON_COSIGN_PUBLIC_KEY
        )
        assert tektonSignatureIntegrationID
        CREATED_SIGNATURE_INTEGRATIONS.put(TEKTON, tektonSignatureIntegrationID)

        // Signature integration "Unverifiable" which holds only the unverifiable cosign public key.
        String unverifiableSignatureIntegrationID = createSignatureIntegration(
                UNVERIFIABLE, UNVERIFIABLE_COSIGN_PUBLIC_KEY
        )
        assert unverifiableSignatureIntegrationID
        CREATED_SIGNATURE_INTEGRATIONS.put(UNVERIFIABLE, unverifiableSignatureIntegrationID)

        // Signature integration "Same+Digest" which holds only the same digest cosign public key.
        String sameDigestSignatureIntegrationID = createSignatureIntegration(
                SAME_DIGEST, SAME_DIGEST_COSIGN_PUBLIC_KEY
        )
        assert sameDigestSignatureIntegrationID
        CREATED_SIGNATURE_INTEGRATIONS.put(SAME_DIGEST, sameDigestSignatureIntegrationID)

        // Signature integration "Distroless+Tekton" which holds both distroless and tekton cosign public keys.
        Map<String,String> mergedKeys = DISTROLESS_PUBLIC_KEY.clone() as Map<String, String>
        mergedKeys.putAll(TEKTON_COSIGN_PUBLIC_KEY.entrySet())
        String distrolessAndTektonSignatureIntegrationID = createSignatureIntegration(
                DISTROLESS_AND_TEKTON, mergedKeys
        )
        assert distrolessAndTektonSignatureIntegrationID
        CREATED_SIGNATURE_INTEGRATIONS.put(DISTROLESS_AND_TEKTON, distrolessAndTektonSignatureIntegrationID)

        // Create all required deployments.
        orchestrator.batchCreateDeployments(DEPLOYMENTS)
        DEPLOYMENTS.each { assert Services.waitForDeployment(it) }

        // Create the policy builders using the signature integration IDs.
        List<Policy.Builder> policyBuilders = []
        for (integrationName in INTEGRATION_NAMES) {
            Policy.Builder builder = createPolicyBuilderWithSignatureCriteria(integrationName,
                    [CREATED_SIGNATURE_INTEGRATIONS.get(integrationName, "")])
            assert builder
            policyBuilders.add(builder)
        }

        // Create a policy which holds three signature integrations.
        Policy.Builder builder = createPolicyBuilderWithSignatureCriteria(POLICY_WITH_DISTROLESS_TEKTON_UNVERIFIABLE,
        [CREATED_SIGNATURE_INTEGRATIONS.get(DISTROLESS), CREATED_SIGNATURE_INTEGRATIONS.get(TEKTON),
         CREATED_SIGNATURE_INTEGRATIONS.get(UNVERIFIABLE)])
        assert builder
        policyBuilders.add(builder)

        // Create policies we use within tests.
        for (policyBuilder in policyBuilders) {
            Policy policy = policyBuilder.build()
            String policyID = PolicyService.createNewPolicy(policy)
            assert policyID
            CREATED_POLICY_IDS.add(policyID)
        }
    }

    def cleanupSpec() {
        // Delete all deployments.
        DEPLOYMENTS.each { orchestrator.deleteAndWaitForDeploymentDeletion(it) }

        // Delete all created policies.
        CREATED_POLICY_IDS.each { PolicyService.deletePolicy(it) }

        // Delete all created signature integrations.
        CREATED_SIGNATURE_INTEGRATIONS.each
                { SignatureIntegrationService.deleteSignatureIntegration(it.value) }

        orchestrator.deleteNamespace(SIGNATURE_TESTING_NAMESPACE)
    }

    def setup() {
        // Reassessing policies will trigger a re-enrichment of images, ensuring we cover potential timeouts occurred
        // during enriching images.
        PolicyService.reassessPolicies()
    }

    @Unroll
    @SuppressWarnings('LineLength')
    @Tag("BAT")
    @Tag("Integration")
    def "Check violations of policy '#policyName' for deployment '#deployment.name'"() {
        expect:
        "Verify deployment has expected violations"
        if (expectViolations) {
            assert waitForViolation(deployment.name, policyName)
        } else {
            assert checkForNoViolations(deployment.name, policyName, 15)
        }

        where:
        policyName                                 | deployment                   | expectViolations
        // Distroless should create alerts for all deployments except those using distroless images.
        DISTROLESS                                 | TEKTON_DEPLOYMENT            | true
        DISTROLESS                                 | UNVERIFIABLE_DEPLOYMENT      | true
        DISTROLESS                                 | WITHOUT_SIGNATURE_DEPLOYMENT | true
        DISTROLESS                                 | DISTROLESS_DEPLOYMENT        | false
        DISTROLESS                                 | SAME_DIGEST_NO_SIGNATURE     | true
        DISTROLESS                                 | SAME_DIGEST_WITH_SIGNATURE   | true
        // Tekton should create alerts for all deployments except those using tekton images.
        TEKTON                                     | DISTROLESS_DEPLOYMENT        | true
        TEKTON                                     | UNVERIFIABLE_DEPLOYMENT      | true
        TEKTON                                     | WITHOUT_SIGNATURE_DEPLOYMENT | true
        TEKTON                                     | TEKTON_DEPLOYMENT            | false
        TEKTON                                     | SAME_DIGEST_NO_SIGNATURE     | true
        TEKTON                                     | SAME_DIGEST_WITH_SIGNATURE   | true
        // Unverifiable should create alerts for all deployments.
        UNVERIFIABLE                               | DISTROLESS_DEPLOYMENT        | true
        UNVERIFIABLE                               | TEKTON_DEPLOYMENT            | true
        UNVERIFIABLE                               | WITHOUT_SIGNATURE_DEPLOYMENT | true
        UNVERIFIABLE                               | UNVERIFIABLE_DEPLOYMENT      | true
        UNVERIFIABLE                               | SAME_DIGEST_NO_SIGNATURE     | true
        UNVERIFIABLE                               | SAME_DIGEST_WITH_SIGNATURE   | true
        // Distroless and tekton should create alerts for all deployments except those using distroless / tekton images.
        DISTROLESS_AND_TEKTON                      | UNVERIFIABLE_DEPLOYMENT      | true
        DISTROLESS_AND_TEKTON                      | WITHOUT_SIGNATURE_DEPLOYMENT | true
        DISTROLESS_AND_TEKTON                      | TEKTON_DEPLOYMENT            | false
        DISTROLESS_AND_TEKTON                      | DISTROLESS_DEPLOYMENT        | false
        DISTROLESS_AND_TEKTON                      | SAME_DIGEST_NO_SIGNATURE     | true
        DISTROLESS_AND_TEKTON                      | SAME_DIGEST_WITH_SIGNATURE   | true
        // Policy with all three integrations should create alerts for all deployments except those using distroless /
        // tekton images.
        POLICY_WITH_DISTROLESS_TEKTON_UNVERIFIABLE | UNVERIFIABLE_DEPLOYMENT      | true
        POLICY_WITH_DISTROLESS_TEKTON_UNVERIFIABLE | WITHOUT_SIGNATURE_DEPLOYMENT | true
        POLICY_WITH_DISTROLESS_TEKTON_UNVERIFIABLE | TEKTON_DEPLOYMENT            | false
        POLICY_WITH_DISTROLESS_TEKTON_UNVERIFIABLE | DISTROLESS_DEPLOYMENT        | false
        POLICY_WITH_DISTROLESS_TEKTON_UNVERIFIABLE | SAME_DIGEST_NO_SIGNATURE     | true
        POLICY_WITH_DISTROLESS_TEKTON_UNVERIFIABLE | SAME_DIGEST_WITH_SIGNATURE   | true
        // Same digest should create alerts for all deployments except those using alt-nginx image.
        SAME_DIGEST                                | UNVERIFIABLE_DEPLOYMENT      | true
        SAME_DIGEST                                | WITHOUT_SIGNATURE_DEPLOYMENT | true
        SAME_DIGEST                                | TEKTON_DEPLOYMENT            | true
        SAME_DIGEST                                | DISTROLESS_DEPLOYMENT        | true
        SAME_DIGEST                                | SAME_DIGEST_NO_SIGNATURE     | true
        SAME_DIGEST                                | SAME_DIGEST_WITH_SIGNATURE   | false
    }

    // Helper which creates a policy builder for a policy which uses the image signature policy criteria.
    private static Policy.Builder createPolicyBuilderWithSignatureCriteria(
            String policyName, List<String> signatureIntegrationIDs) {
        def builder = BASE_POLICY.clone().setName(policyName)
        def policyGroup = PolicyOuterClass.PolicyGroup.newBuilder()
                .setFieldName("Image Signature Verified By")
                .setBooleanOperator(PolicyOuterClass.BooleanOperator.OR)
        policyGroup.addAllValues(
                signatureIntegrationIDs.collect
                        { PolicyOuterClass.PolicyValue.newBuilder().setValue(it).build() })
                .setNegate(false)
                .build()
        def policyBuilder = builder.addPolicySections(
                PolicyOuterClass.PolicySection.newBuilder().addPolicyGroups(policyGroup.build()).build()
        )
        return policyBuilder
    }

    // Helper to create a signature integration with given name and public keys.
    private static String createSignatureIntegration(String integrationName, Map<String, String> namedPublicKeys) {
        String signatureIntegrationID = SignatureIntegrationService.createSignatureIntegration(
                SignatureIntegration.newBuilder()
                        .setName(integrationName)
                        .setCosign(CosignPublicKeyVerification.newBuilder()
                                .addAllPublicKeys(namedPublicKeys.collect
                                        {
                                            CosignPublicKeyVerification.PublicKey.newBuilder()
                                                    .setName(it.key).setPublicKeyPemEnc(it.value)
                                                    .build()
                                        })
                                .build()
                        )
                        .build()
        )
        return signatureIntegrationID
    }
}
